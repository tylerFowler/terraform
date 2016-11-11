package template

// TODO:
// - add `users` section schema def
// - write render fn
// - add options for gzipping & encoding in base64
// - fill in remaining options for etcd & etcd2 schemas

import (
	"bytes"
	"fmt"
	"strconv"

	"github.com/hashicorp/terraform/helper/hashcode"
	"github.com/hashicorp/terraform/helper/schema"
)

type systemdDropin struct {
	name    string
	content *string
}

type systemdUnit struct {
	name    string
	content *string
	runtime bool
	enable  bool
	command string
	mask    bool
	dropins []*systemdDropin
}

func dataSourceCoreOSCloudinit() *schema.Resource {
	return &schema.Resource{
		Read: dataSourceCoreOSCloudinitRead,
		Schema: map[string]*schema.Schema{
			"use_shebang": &schema.Schema{
				Description: "Whether or not to use the shebang (`#` vs `#!`) in the #cloud-config directive",
				Type:        schema.TypeBool,
				Optional:    true,
				Default:     false,
			},
			"hostname": &schema.Schema{
				Type:     schema.TypeString,
				Optional: true,
			},
			"ssh_authorized_keys": &schema.Schema{
				Type:     schema.TypeList,
				Optional: true,
				Elem:     &schema.Schema{Type: schema.TypeString},
			},
			"manage_etc_hosts": &schema.Schema{
				Type:         schema.TypeString,
				Optional:     true,
				ValidateFunc: etcHostsValidation,
			},
			"update_strategy": updateSchema,
			"etcd":            etcdSchema,
			"etcd2":           etcd2Schema,
			"fleet":           fleetSchema,
			"flannel":         flannelSchema,
			"locksmith":       locksmithSchema,
			"systemd_unit":    systemdUnitSchema,
			"write_file":      writeFileSchema,
			"rendered": &schema.Schema{
				Type:        schema.TypeString,
				Computed:    true,
				Description: "rendered cloud-config file",
			},
		},
	}
}

func dataSourceCoreOSCloudinitRead(data *schema.ResourceData, meta interface{}) error {
	rendered, err := renderCloudinit(data)
	if err != nil {
		return err
	}

	data.Set("rendered", rendered)
	data.SetId(strconv.Itoa(hashcode.String(rendered)))
	return nil
}

// renderCloudinit finds the various parts of the cloud-config and calls functions to
// render each one
func renderCloudinit(data *schema.ResourceData) (string, error) {
	var cloudinitBuf bytes.Buffer

	useShebang := data.Get("use_shebang").(bool)

	// write the cloud-config header
	if useShebang == true {
		cloudinitBuf.WriteString("#!cloud-config")
	} else {
		cloudinitBuf.WriteString("#cloud-config\n")
	}

	// write the hostname
	if hostname, hasHostname := data.GetOk("hostname"); hasHostname {
		cloudinitBuf.WriteString(fmt.Sprintf("hostname: %s\n", hostname.(string)))
	}

	// write the ssh_authorized_keys
	if sshAuthKeys, hasSSHKeys := data.GetOk("ssh_authorized_keys"); hasSSHKeys {
		cloudinitBuf.WriteString("ssh_authorized_keys:\n")
		for _, sshKey := range sshAuthKeys.([]interface{}) {
			cloudinitBuf.WriteString(fmt.Sprintf("\t- %s", sshKey.(string)))
		}
	}

	// write the manage_etc_hosts entries
	if etcHosts, hasEtcHosts := data.GetOk("manage_ssh_hosts"); hasEtcHosts {
		cloudinitBuf.WriteString(fmt.Sprintf("manage_ssh_hosts: %s\n", etcHosts.(string)))
	}

	// write the coreos bits if applicable
	writeCoreosValues(&cloudinitBuf, data)

	// write the systemd units
	if writeErr := writeSystemdUnits(&cloudinitBuf, data); writeErr != nil {
		return "", writeErr
	}

	// write the write_file directives

	return cloudinitBuf.String(), nil
}

// writeCoreosValues writes the data at the CoreOS native fields wherever
// applicable, otherwise it will write nothing to the buffer
func writeCoreosValues(buffer *bytes.Buffer, data *schema.ResourceData) {
	// write the coreos key & set indentation level
	buffer.WriteString("coreos:\n")

	// check for update strategy values
	if updateStrategy, hasUpdateStrategy := data.GetOk("update_strategy"); hasUpdateStrategy {
		d := updateStrategy.(map[string]interface{})
		buffer.WriteString("\tupdate:")

		if rebootStrategy, ok := d["reboot_strategy"]; ok {
			buffer.WriteString(fmt.Sprintf("\t\treboot_strategy: %s", rebootStrategy))
		}

		if server, ok := d["server"]; ok {
			buffer.WriteString(fmt.Sprintf("\t\tserver: %s", server))
		}

		if group, ok := d["group"]; ok {
			buffer.WriteString(fmt.Sprintf("\t\tgroup: %s", group))
		}
	}

	// check for etcd values
	// check for etcd2 values
	// check for fleet values
	// check for flannel values
	// check for locksmith values
}

// writeSystemdUnits extracts all systemd unit directives (coreos.units.*) from the cloud-config data
// and writes them to the given buffer, if there are any
func writeSystemdUnits(buf *bytes.Buffer, data *schema.ResourceData) error {
	unitValue, hasUnits := data.GetOk("systemd_unit")
	if !hasUnits {
		return nil
	}

	// build each systemd unit & send it off to be written to the buffer
	for _, val := range unitValue.([]interface{}) {
		rawUnit := val.(map[string]interface{})
		unit := systemdUnit{}

		if p, ok := rawUnit["name"]; ok {
			unit.name = p.(string)
		}

		if p, ok := rawUnit["content"]; ok {
			cntnt := p.(string)
			unit.content = &cntnt
		}

		if p, ok := rawUnit["runtime"]; ok {
			unit.runtime = p.(bool)
		}

		if p, ok := rawUnit["enable"]; ok {
			unit.enable = p.(bool)
		}

		if p, ok := rawUnit["command"]; ok {
			unit.command = p.(string)
		}

		if p, ok := rawUnit["mask"]; ok {
			unit.mask = p.(bool)
		}

		// TODO: process all dropins & add them the schema
		unit.dropins = []*systemdDropin{}

		// TODO: if possible we should use the `go` keyword on this
		// - would have to wrap the buffer into a custom thread-safe type using a mutex
		// - consider using io.Pipe on buffers as it is thread safe
		if writeErr := writeSystemdUnit(buf, &unit); writeErr == nil {
			return writeErr
		}
	}

	return nil
}

// writeSystemdUnit appends the given systemd unit definition to the given buffer
func writeSystemdUnit(buf *bytes.Buffer, unitDef *systemdUnit) error {
	// TODO: implement
	return nil
}

// CoreOS Key schemas

// etcdSchema maps to coreos: etcd
// TODO: add remaining configuration options
var etcdSchema = &schema.Schema{
	Type:       schema.TypeMap,
	Optional:   true,
	Deprecated: "The etcd block has been deprecated by CoreOS in favor of etcd2",
	Elem: &schema.Resource{
		Schema: map[string]*schema.Schema{
			"name":      &schema.Schema{Type: schema.TypeString, Required: true},
			"discovery": &schema.Schema{Type: schema.TypeString, Optional: true},
			"addr":      &schema.Schema{Type: schema.TypeString, Optional: true},
			"peer_addr": &schema.Schema{Type: schema.TypeString, Optional: true},
		},
	},
}

// etcd2Schema maps to coreos: etcd2
// TODO: add remaining configuration options
var etcd2Schema = &schema.Schema{
	Type:     schema.TypeMap,
	Optional: true,
	Elem: &schema.Resource{
		Schema: map[string]*schema.Schema{
			"discovery":                   &schema.Schema{Type: schema.TypeString, Optional: true},
			"advertise_client_urls":       &schema.Schema{Type: schema.TypeString, Optional: true},
			"initial_advertise_peer_urls": &schema.Schema{Type: schema.TypeString, Optional: true},
			"listen_client_urls":          &schema.Schema{Type: schema.TypeString, Optional: true},
			"listen_peer_urls":            &schema.Schema{Type: schema.TypeString, Optional: true},
		},
	},
}

// fleetSchema maps to coreos: fleet
var fleetSchema = &schema.Schema{
	Type:     schema.TypeMap,
	Optional: true,
	Elem: &schema.Resource{
		Schema: map[string]*schema.Schema{
			"agent_ttl":                 &schema.Schema{Type: schema.TypeInt, Optional: true},
			"engine_reconcile_interval": &schema.Schema{Type: schema.TypeInt, Optional: true},
			"etcd_cafile":               &schema.Schema{Type: schema.TypeString, Optional: true},
			"etcd_certfile":             &schema.Schema{Type: schema.TypeString, Optional: true},
			"etcd_keyfile":              &schema.Schema{Type: schema.TypeString, Optional: true},
			"etcd_request_timeout":      &schema.Schema{Type: schema.TypeInt, Optional: true},
			"etcd_servers": &schema.Schema{
				Type:     schema.TypeList,
				Optional: true,
				Elem:     &schema.Schema{Type: schema.TypeString},
			},
			"metadata": &schema.Schema{
				Type:     schema.TypeList,
				Optional: true,
				Elem:     &schema.Schema{Type: schema.TypeMap}, // TODO: will this allow us to put in arbitrary key/value pairs?
			},
			"verbosity": &schema.Schema{Type: schema.TypeInt, Optional: true},
		},
	},
}

// flannelSchema maps to coreos: flannel
var flannelSchema = &schema.Schema{
	Type:     schema.TypeMap,
	Optional: true,
	Elem: &schema.Resource{
		Schema: map[string]*schema.Schema{
			"etcd_endpoints": &schema.Schema{
				Type:     schema.TypeList,
				Optional: true,
				Elem:     &schema.Schema{Type: schema.TypeString},
			},
			"etcd_cafile":   &schema.Schema{Type: schema.TypeString, Optional: true},
			"etcd_certfile": &schema.Schema{Type: schema.TypeString, Optional: true},
			"etcd_keyfile":  &schema.Schema{Type: schema.TypeString, Optional: true},
			"etcd_prefix":   &schema.Schema{Type: schema.TypeString, Optional: true},
			"ip_masq":       &schema.Schema{Type: schema.TypeString, Optional: true},
			"subnet_file":   &schema.Schema{Type: schema.TypeString, Optional: true},
			"interface":     &schema.Schema{Type: schema.TypeString, Optional: true},
			"public_ip":     &schema.Schema{Type: schema.TypeString, Optional: true},
		},
	},
}

// locksmithSchema maps to coreos: locksmith
var locksmithSchema = &schema.Schema{
	Type:     schema.TypeMap,
	Optional: true,
	Elem: &schema.Resource{
		Schema: map[string]*schema.Schema{
			"endpoint": &schema.Schema{
				Type:     schema.TypeList,
				Optional: true,
				Elem:     &schema.Schema{Type: schema.TypeString},
			},
			"etcd_cafile":   &schema.Schema{Type: schema.TypeString, Optional: true},
			"etcd_certfile": &schema.Schema{Type: schema.TypeString, Optional: true},
			"etcd_keyfile":  &schema.Schema{Type: schema.TypeString, Optional: true},
			"group":         &schema.Schema{Type: schema.TypeString, Optional: true},
			"window_start":  &schema.Schema{Type: schema.TypeString, Optional: true},
			"window_length": &schema.Schema{Type: schema.TypeString, Optional: true},
		},
	},
}

// updateSchema maps to coreos: update
var updateSchema = &schema.Schema{
	Type:     schema.TypeMap,
	Optional: true,
	Elem: &schema.Resource{
		Schema: map[string]*schema.Schema{
			"reboot_strategy": &schema.Schema{
				Type:     schema.TypeString,
				Optional: true,
				ValidateFunc: func(val interface{}, key string) (_ []string, errors []error) {
					value := val.(string)
					if value != "reboot" && value != "etcd-lock" && value != "best-effort" && value != "off" {
						errors = append(errors, fmt.Errorf("Reboot strategy must be one of 'reboot', 'etcd-lock', 'best-effort', or 'off'"))
					}

					return
				},
			},
			"server": &schema.Schema{Type: schema.TypeString, Optional: true},
			"group":  &schema.Schema{Type: schema.TypeString, Optional: true},
		},
	},
}

// systemdUnitSchema maps to the coreos: unit key
var systemdUnitSchema = &schema.Schema{
	Type:     schema.TypeList,
	Optional: true,
	Elem: &schema.Resource{
		Schema: map[string]*schema.Schema{
			"name":    &schema.Schema{Type: schema.TypeString, Required: true},
			"content": &schema.Schema{Type: schema.TypeString, Required: true},
			"runtime": &schema.Schema{Type: schema.TypeBool, Optional: true},
			"enable":  &schema.Schema{Type: schema.TypeBool, Optional: true},
			"command": &schema.Schema{Type: schema.TypeString, Optional: true},
			"mask":    &schema.Schema{Type: schema.TypeBool, Optional: true},
			"dropin": &schema.Schema{ // TODO: can we nest maps like this?
				Type:     schema.TypeList,
				Optional: true,
				Elem: &schema.Resource{
					Schema: map[string]*schema.Schema{
						"name":    &schema.Schema{Type: schema.TypeString, Required: true},
						"content": &schema.Schema{Type: schema.TypeString, Required: true},
					},
				},
			},
		},
	},
}

// writeFileSchema maps to the write_files: key
var writeFileSchema = &schema.Schema{
	Type:     schema.TypeList,
	Optional: true,
	Elem: &schema.Resource{
		Schema: map[string]*schema.Schema{
			"path":        &schema.Schema{Type: schema.TypeString, Required: true},
			"content":     &schema.Schema{Type: schema.TypeString, Required: true},
			"permissions": &schema.Schema{Type: schema.TypeString, Optional: true},
			"owner":       &schema.Schema{Type: schema.TypeString, Optional: true},
			"encoding":    &schema.Schema{Type: schema.TypeString, Optional: true},
		},
	},
}

// Validation functions

// etcHostsValidation runs validation on the cloud-config's `manage_etc_hosts` key,
// currently CoreOS only supports a value of "localhost", so throw a warning when any other
// value is given
func etcHostsValidation(val interface{}, key string) (warnings []string, _ []error) {
	value := val.(string)
	if value != "localhost" {
		warnings = append(warnings, "Manage etc hosts currently only supports values of 'localhost' in CoreOS")
	}

	return
}
