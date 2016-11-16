package template

// TODO:
// - add options for gzipping & encoding in base64
// - if `coreos: ` ends up being empty we need to not write it (or write something ineffectual to it)

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"strconv"
	"strings"

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
			"user":            userSchema,
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
		cloudinitBuf.WriteString("#!cloud-config\n")
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
			cloudinitBuf.WriteString(fmt.Sprintf("\t- %q\n", sshKey.(string)))
		}
	}

	// write the manage_etc_hosts entries
	if etcHosts, hasEtcHosts := data.GetOk("manage_etc_hosts"); hasEtcHosts {
		cloudinitBuf.WriteString(fmt.Sprintf("manage_etc_hosts: %s\n", etcHosts.(string)))
	}

	// write the coreos key regardless of whether or not it has values
	cloudinitBuf.WriteString("coreos:\n")

	// write the coreos bits if applicable
	if writeErr := writeCoreosValues(&cloudinitBuf, data); writeErr != nil {
		return "", writeErr
	}

	// write the systemd units
	if writeErr := writeSystemdUnits(&cloudinitBuf, data); writeErr != nil {
		return "", writeErr
	}

	// write the write_file directives
	if writeErr := writeWriteFiles(&cloudinitBuf, data); writeErr != nil {
		return "", writeErr
	}

	if writeErr := writeUsers(&cloudinitBuf, data); writeErr != nil {
		return "", writeErr
	}

	// replace all tabs with soft spaces since YAML doesn't like tabs
	// TODO: eventually want to do this earlier in the process so that we can avoid
	// changing the content of user scripts and such
	return strings.Replace(cloudinitBuf.String(), "\t", "  ", -1), nil
}

// writeCoreosValues writes the data at the CoreOS native fields wherever
// applicable, otherwise it will write nothing to the buffer
func writeCoreosValues(buf *bytes.Buffer, data *schema.ResourceData) error {
	var err error

	if etcdConf, ok := data.GetOk("etcd"); ok {
		err = writeCoreosDirective(buf, "etcd", etcdConf, etcdSchema)
	}

	if etcd2Conf, ok := data.GetOk("etcd2"); ok {
		err = writeCoreosDirective(buf, "etcd2", etcd2Conf, etcd2Schema)
	}

	if fleetConf, ok := data.GetOk("fleet"); ok {
		err = writeCoreosDirective(buf, "fleet", fleetConf, fleetSchema)
	}

	if flannelConf, ok := data.GetOk("flannel"); ok {
		err = writeCoreosDirective(buf, "flannel", flannelConf, flannelSchema)
	}

	if locksmithConf, ok := data.GetOk("locksmith"); ok {
		err = writeCoreosDirective(buf, "locksmith", locksmithConf, locksmithSchema)
	}

	if updateStrategy, ok := data.GetOk("update_strategy"); ok {
		err = writeCoreosDirective(buf, "update", updateStrategy, updateSchema)
	}

	return err
}

// writeCoreosDirective writes a single coreos.* directive, using `directiveName` as the name,
// and parsing `vals` and referencing the given schema to figure out how to display
// it in YAML format
func writeCoreosDirective(
	buf *bytes.Buffer, directiveName string, rawVals interface{}, dirSchema *schema.Schema,
) error {
	writeKey := func(k string, v string) {
		buf.WriteString(fmt.Sprintf("\t\t%s: %s\n", k, v))
	}

	vals, typeOk := rawVals.(map[string]interface{})
	if !typeOk {
		return fmt.Errorf(
			"Could not write CoreOS directive %s, values %v could not be casted to `map[string]interface{}`",
			directiveName,
			vals,
		)
	}

	if len(vals) == 0 {
		return nil
	}

	buf.WriteString(fmt.Sprintf("\t%s:\n", directiveName))
	for key, val := range vals {
		// find the key in the schema
		keySchema, ok := dirSchema.Elem.(*schema.Resource).Schema[key]
		if ok == false {
			return fmt.Errorf("Directive %s unable to get key schema for key %s from %v", directiveName, key, dirSchema)
		}

		// convert the key to the cloud-config format by replacing
		// underscores (_) with dashes (-)
		cloudinitKey := strings.Replace(key, "_", "-", -1)

		switch keySchema.Type {
		// strings are simply written as key: "strVal"
		case schema.TypeString:
			writeKey(cloudinitKey, fmt.Sprintf("%q", val.(string)))
		// ints are parsed as strings w/o quotes
		case schema.TypeInt:
			writeKey(cloudinitKey, fmt.Sprintf("%s", val.(string)))
		// bools are written out as true or false, and apparently are given as a string
		// containing "0" for false and "1" for true presumably because we're using a schema.TypeMap for these
		case schema.TypeBool:
			if val.(string) == "0" {
				writeKey(cloudinitKey, "false")
			} else if val.(string) == "1" {
				writeKey(cloudinitKey, "true")
			}
		// lists are joined together as comma separated strings
		case schema.TypeList:
			writeKey(cloudinitKey, strings.Join(val.([]string), ","))
		}
	}

	return nil
}

// writeSystemdUnits extracts all systemd unit directives (coreos.units.*) from the cloud-config data
// and writes them to the given buffer, if there are any
func writeSystemdUnits(buf *bytes.Buffer, data *schema.ResourceData) error {
	unitValue, hasUnits := data.GetOk("systemd_unit")
	if !hasUnits {
		return nil
	}

	// add the units: (assumes buffer cursor is below the "coreos:" key)
	buf.WriteString("\tunits:\n")

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

		unit.dropins = []*systemdDropin{}
		if p, ok := rawUnit["dropin"]; ok {
			ds := p.([]interface{})

			for _, d := range ds {
				rawDropin := d.(map[string]interface{})
				dropin := systemdDropin{}

				if dName, hasName := rawDropin["name"]; hasName {
					dropin.name = dName.(string)
				}

				if dContent, hasContent := rawDropin["content"]; hasContent {
					cntnt := dContent.(string)
					dropin.content = &cntnt
				}

				unit.dropins = append(unit.dropins, &dropin)
			}
		}

		if writeErr := writeSystemdUnit(buf, &unit); writeErr != nil {
			return writeErr
		}
	}

	return nil
}

// writeSystemdUnit appends the given systemd unit definition to the given buffer
func writeSystemdUnit(buf *bytes.Buffer, unitDef *systemdUnit) error {
	writeUnitKey := func(ln string) {
		buf.WriteString(fmt.Sprintf("\t\t\t%v\n", ln))
	}

	if unitDef.content == nil || *unitDef.content == "" && len(unitDef.dropins) == 0 {
		return errors.New("Systemd units without any content must have at least one dropin")
	}

	buf.WriteString(fmt.Sprintf("\t\t- name: %s\n", unitDef.name))

	// for the boolean values, they all default in CoreOS to false so only write
	// the keys if they are true
	if unitDef.runtime == true {
		writeUnitKey("runtime: true")
	}

	if unitDef.mask == true {
		writeUnitKey("mask: true")
	}

	if unitDef.enable == true {
		writeUnitKey("enable: true")
	}

	if unitDef.command != "" {
		writeUnitKey(fmt.Sprintf("command: %s", unitDef.command))
	}

	if unitDef.content != nil && *unitDef.content != "" {
		writeUnitKey("content: |")
		buf.WriteString(indentString(4, unitDef.content))
	}

	// write the drop-ins, if any
	if len(unitDef.dropins) == 0 {
		return nil
	}

	writeUnitKey("drop-ins:")
	for _, dropin := range unitDef.dropins {
		buf.WriteString(fmt.Sprintf("\t\t\t\t- name: %s\n", dropin.name))
		buf.WriteString("\t\t\t\t\tcontent: |\n")
		buf.WriteString(indentString(6, dropin.content))
	}

	return nil
}

// writeWriteFiles appends all write file directives to the cloud-config,
// if there are any
func writeWriteFiles(buf *bytes.Buffer, data *schema.ResourceData) error {
	writeDirectiveKey := func(d string) {
		buf.WriteString(fmt.Sprintf("\t\t%v\n", d))
	}

	writeFilesVal, hasWriteFiles := data.GetOk("write_file")
	if !hasWriteFiles {
		return nil
	}

	buf.WriteString("write_files:\n")

	for _, val := range writeFilesVal.([]interface{}) {
		rawVal := val.(map[string]interface{})

		if p, ok := rawVal["path"]; ok && p.(string) != "" {
			buf.WriteString(fmt.Sprintf("\t- path: %q\n", p.(string)))
		} else { // ensure we always have this initial key
			return errors.New("`write_file` block must have a path")
		}

		if p, ok := rawVal["permissions"]; ok && p.(string) != "" {
			writeDirectiveKey(fmt.Sprintf("permissions: %s", p.(string)))
		}

		if p, ok := rawVal["owner"]; ok && p.(string) != "" {
			writeDirectiveKey(fmt.Sprintf("owner: %s", p.(string)))
		}

		if p, ok := rawVal["encoding"]; ok && p.(string) != "" {
			writeDirectiveKey(fmt.Sprintf("encoding: %s", p.(string)))
		}

		if p, ok := rawVal["content"]; ok && p.(string) != "" {
			cntnt := p.(string)
			writeDirectiveKey("content: |")
			buf.WriteString(indentString(3, &cntnt))
		}
	}

	return nil
}

func writeUsers(buf *bytes.Buffer, data *schema.ResourceData) error {
	writeUserKey := func(key string, value string) {
		buf.WriteString(fmt.Sprintf("\t\t%s: %v\n", key, value))
	}

	usersVal, hasUsers := data.GetOk("user")
	if !hasUsers {
		return nil
	}

	buf.WriteString("users:\n")

	for _, v := range usersVal.([]interface{}) {
		rawUser := v.(map[string]interface{})

		// we must always write the name key first
		buf.WriteString(fmt.Sprintf("\t- name: %q\n", rawUser["name"].(string)))

		for userKey, userVal := range rawUser {
			if userKey == "name" { // already processed the name key
				continue
			}

			keySchema, keyCheckOk := userSchema.Elem.(*schema.Resource).Schema[userKey]
			if keyCheckOk == false {
				return fmt.Errorf("User section unable to get key schema for key %s from %v", userKey, userSchema)
			}

			cloudinitKey := strings.Replace(userKey, "_", "-", -1)
			switch keySchema.Type {
			case schema.TypeString:
				if userVal.(string) != "" {
					writeUserKey(cloudinitKey, userVal.(string))
				}
			// TODO: for some of these boolean values, doing true *or* false will change the behavior,
			// so we need a way to "unset" boolean values that user did not explicitly specify
			case schema.TypeBool:
				fmt.Printf("=== Got a bool: %v", userVal)
				if userVal.(bool) != false {
					writeUserKey(cloudinitKey, fmt.Sprintf("%t", userVal.(bool)))
				}
			case schema.TypeList:
				ls := userVal.([]interface{})
				if len(ls) == 0 {
					continue
				}

				writeUserKey(cloudinitKey, "") // write a blank key line the main list key
				for _, l := range ls {
					buf.WriteString(fmt.Sprintf("\t\t\t- %q\n", l.(string)))
				}
			}
		}
	}

	return nil
}

// indentString inserts the given number of tabs in front of every line of the string, returning
// the indented string, it will *not* write tabs to blank new lines
func indentString(indentLvl int, str *string) string {
	var buf bytes.Buffer
	sc := bufio.NewScanner(strings.NewReader(*str))
	sc.Split(bufio.ScanLines)

	for sc.Scan() {
		line := sc.Text()

		// if our line consists only of "\n" (in which case the given line is empty) then don't indent it
		if len(line) == 0 {
			buf.WriteString("\n")
		} else {
			buf.WriteString(fmt.Sprintf("%s%s\n", strings.Repeat("\t", indentLvl), line))
		}
	}

	return buf.String()
}

// CoreOS Key schemas

// etcdSchema maps to coreos: etcd
var etcdSchema = &schema.Schema{
	Type:       schema.TypeMap,
	Optional:   true,
	Deprecated: "The etcd block has been deprecated by CoreOS in favor of etcd2",
	Elem: &schema.Resource{
		Schema: map[string]*schema.Schema{
			"name":                    &schema.Schema{Type: schema.TypeString, Required: true},
			"addr":                    &schema.Schema{Type: schema.TypeString, Optional: true},
			"discovery":               &schema.Schema{Type: schema.TypeString, Optional: true},
			"http_read_timeout":       &schema.Schema{Type: schema.TypeInt, Optional: true},
			"http_write_timeout":      &schema.Schema{Type: schema.TypeInt, Optional: true},
			"bind_addr":               &schema.Schema{Type: schema.TypeString, Optional: true},
			"peers":                   &schema.Schema{Type: schema.TypeString, Optional: true},
			"ca_file":                 &schema.Schema{Type: schema.TypeString, Optional: true},
			"cert_file":               &schema.Schema{Type: schema.TypeString, Optional: true},
			"key_file":                &schema.Schema{Type: schema.TypeString, Optional: true},
			"cors":                    &schema.Schema{Type: schema.TypeString, Optional: true},
			"data_dir":                &schema.Schema{Type: schema.TypeString, Optional: true},
			"max_result_buffer":       &schema.Schema{Type: schema.TypeInt, Optional: true},
			"max_retry_attempts":      &schema.Schema{Type: schema.TypeInt, Optional: true},
			"peer_addr":               &schema.Schema{Type: schema.TypeString, Optional: true},
			"peer_bind_addr":          &schema.Schema{Type: schema.TypeString, Optional: true},
			"peer_ca_file":            &schema.Schema{Type: schema.TypeString, Optional: true},
			"peer_cert_file":          &schema.Schema{Type: schema.TypeString, Optional: true},
			"peer_key_file":           &schema.Schema{Type: schema.TypeString, Optional: true},
			"peer_election_timeout":   &schema.Schema{Type: schema.TypeInt, Optional: true},
			"peer_heartbeat_interval": &schema.Schema{Type: schema.TypeInt, Optional: true},
			"snapshot":                &schema.Schema{Type: schema.TypeBool, Optional: true},
			"cluster_active_size":     &schema.Schema{Type: schema.TypeInt, Optional: true},
			"cluster_remove_delay":    &schema.Schema{Type: schema.TypeInt, Optional: true},
			"cluster_sync_interval":   &schema.Schema{Type: schema.TypeInt, Optional: true},
		},
	},
}

// etcd2Schema maps to coreos: etcd2
var etcd2Schema = &schema.Schema{
	Type:     schema.TypeMap,
	Optional: true,
	Elem: &schema.Resource{
		Schema: map[string]*schema.Schema{
			"name":               &schema.Schema{Type: schema.TypeString, Optional: true},
			"data_dir":           &schema.Schema{Type: schema.TypeString, Optional: true},
			"wal_dir":            &schema.Schema{Type: schema.TypeString, Optional: true},
			"snapshot_count":     &schema.Schema{Type: schema.TypeInt, Optional: true},
			"heartbeat_interval": &schema.Schema{Type: schema.TypeInt, Optional: true},
			"election_timeout":   &schema.Schema{Type: schema.TypeInt, Optional: true},
			"listen_peer_urls":   &schema.Schema{Type: schema.TypeString, Optional: true},
			"listen_client_urls": &schema.Schema{Type: schema.TypeString, Optional: true},
			"max_snapshots":      &schema.Schema{Type: schema.TypeInt, Optional: true},
			"max_wals":           &schema.Schema{Type: schema.TypeInt, Optional: true},
			"cors":               &schema.Schema{Type: schema.TypeString, Optional: true},
			"initial_advertise_peer_urls": &schema.Schema{Type: schema.TypeString, Optional: true},
			"initial_cluster":             &schema.Schema{Type: schema.TypeString, Optional: true},
			"initial_cluster_state":       &schema.Schema{Type: schema.TypeString, Optional: true},
			"initial_cluster_token":       &schema.Schema{Type: schema.TypeString, Optional: true},
			"advertise_client_urls":       &schema.Schema{Type: schema.TypeString, Optional: true},
			"discovery":                   &schema.Schema{Type: schema.TypeString, Optional: true},
			"discovery_srv":               &schema.Schema{Type: schema.TypeString, Optional: true},
			"discovery_fallback":          &schema.Schema{Type: schema.TypeString, Optional: true},
			"discovery_proxy":             &schema.Schema{Type: schema.TypeString, Optional: true},
			"strict_reconfig_check":       &schema.Schema{Type: schema.TypeBool, Optional: true},
			"proxy":                       &schema.Schema{Type: schema.TypeString, Optional: true},
			"proxy_failure_wait":          &schema.Schema{Type: schema.TypeInt, Optional: true},
			"proxy_refresh_interval":      &schema.Schema{Type: schema.TypeInt, Optional: true},
			"proxy_dial_timeout":          &schema.Schema{Type: schema.TypeInt, Optional: true},
			"proxy_write_timeout":         &schema.Schema{Type: schema.TypeInt, Optional: true},
			"proxy_read_timeout":          &schema.Schema{Type: schema.TypeInt, Optional: true},
			"ca_file":                     &schema.Schema{Type: schema.TypeString, Optional: true, Deprecated: "Recommended to use trusted_ca_file and client_cert_auth"},
			"cert_file":                   &schema.Schema{Type: schema.TypeString, Optional: true},
			"key_file":                    &schema.Schema{Type: schema.TypeString, Optional: true},
			"client_cert_auth":            &schema.Schema{Type: schema.TypeBool, Optional: true},
			"trusted_ca_file":             &schema.Schema{Type: schema.TypeString, Optional: true},
			"peer_ca_file":                &schema.Schema{Type: schema.TypeString, Optional: true, Deprecated: "Recommended to use peer_trusted_ca_file and peer_client_cert_auth"},
			"peer_cert_file":              &schema.Schema{Type: schema.TypeString, Optional: true},
			"peer_key_file":               &schema.Schema{Type: schema.TypeString, Optional: true},
			"peer_client_cert_auth":       &schema.Schema{Type: schema.TypeBool, Optional: true},
			"peer_trusted_ca_file":        &schema.Schema{Type: schema.TypeString, Optional: true},
			"debug":                       &schema.Schema{Type: schema.TypeBool, Optional: true},
			"log_package_levels":          &schema.Schema{Type: schema.TypeString, Optional: true},
			"force_new_cluster":           &schema.Schema{Type: schema.TypeBool, Optional: true},
			"enable_pprof":                &schema.Schema{Type: schema.TypeBool, Optional: true},
		},
	},
}

// fleetSchema maps to coreos: fleet
var fleetSchema = &schema.Schema{
	Type:     schema.TypeMap,
	Optional: true,
	Elem: &schema.Resource{
		Schema: map[string]*schema.Schema{
			"public_ip":                 &schema.Schema{Type: schema.TypeString, Optional: true},
			"agent_ttl":                 &schema.Schema{Type: schema.TypeInt, Optional: true},
			"engine_reconcile_interval": &schema.Schema{Type: schema.TypeInt, Optional: true},
			"etcd_cafile":               &schema.Schema{Type: schema.TypeString, Optional: true},
			"etcd_certfile":             &schema.Schema{Type: schema.TypeString, Optional: true},
			"etcd_keyfile":              &schema.Schema{Type: schema.TypeString, Optional: true},
			"etcd_request_timeout":      &schema.Schema{Type: schema.TypeInt, Optional: true},
			"etcd_servers":              &schema.Schema{Type: schema.TypeString, Optional: true},
			"metadata":                  &schema.Schema{Type: schema.TypeString, Optional: true},
			"verbosity":                 &schema.Schema{Type: schema.TypeInt, Optional: true},
		},
	},
}

// flannelSchema maps to coreos: flannel
var flannelSchema = &schema.Schema{
	Type:     schema.TypeMap,
	Optional: true,
	Elem: &schema.Resource{
		Schema: map[string]*schema.Schema{
			"etcd_endpoints": &schema.Schema{Type: schema.TypeString, Optional: true},
			"etcd_cafile":    &schema.Schema{Type: schema.TypeString, Optional: true},
			"etcd_certfile":  &schema.Schema{Type: schema.TypeString, Optional: true},
			"etcd_keyfile":   &schema.Schema{Type: schema.TypeString, Optional: true},
			"etcd_prefix":    &schema.Schema{Type: schema.TypeString, Optional: true},
			"ip_masq":        &schema.Schema{Type: schema.TypeString, Optional: true},
			"subnet_file":    &schema.Schema{Type: schema.TypeString, Optional: true},
			"interface":      &schema.Schema{Type: schema.TypeString, Optional: true},
			"public_ip":      &schema.Schema{Type: schema.TypeString, Optional: true},
		},
	},
}

// locksmithSchema maps to coreos: locksmith
var locksmithSchema = &schema.Schema{
	Type:     schema.TypeMap,
	Optional: true,
	Elem: &schema.Resource{
		Schema: map[string]*schema.Schema{
			"endpoint":      &schema.Schema{Type: schema.TypeString, Optional: true},
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

// userSchema maps to top level user values
var userSchema = &schema.Schema{
	Type:     schema.TypeList,
	Optional: true,
	Elem: &schema.Resource{
		Schema: map[string]*schema.Schema{
			"name":                           &schema.Schema{Type: schema.TypeString, Required: true},
			"gecos":                          &schema.Schema{Type: schema.TypeString, Optional: true},
			"passwd":                         &schema.Schema{Type: schema.TypeString, Optional: true},
			"homedir":                        &schema.Schema{Type: schema.TypeString, Optional: true},
			"no_create_home":                 &schema.Schema{Type: schema.TypeBool, Optional: true},
			"primary_group":                  &schema.Schema{Type: schema.TypeString, Optional: true},
			"no_user_group":                  &schema.Schema{Type: schema.TypeBool, Optional: true},
			"coreos_ssh_import_github":       &schema.Schema{Type: schema.TypeString, Optional: true, Deprecated: "Deprecated by CoreOS"},
			"coreos_ssh_import_github_users": &schema.Schema{Type: schema.TypeString, Optional: true, Deprecated: "Deprecated by CoreOS"},
			"coreos_ssh_import_url":          &schema.Schema{Type: schema.TypeString, Optional: true, Deprecated: "Deprecated by CoreOS"},
			"system":                         &schema.Schema{Type: schema.TypeBool, Optional: true},
			"no_log_init":                    &schema.Schema{Type: schema.TypeBool, Optional: true},
			"shell":                          &schema.Schema{Type: schema.TypeString, Optional: true},
			"groups": &schema.Schema{
				Type:     schema.TypeList,
				Optional: true,
				Elem:     &schema.Schema{Type: schema.TypeString},
			},
			"ssh_authorized_keys": &schema.Schema{
				Type:     schema.TypeList,
				Optional: true,
				Elem:     &schema.Schema{Type: schema.TypeString},
			},
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
			"content": &schema.Schema{Type: schema.TypeString, Optional: true}, // required if we have no drop ins
			"runtime": &schema.Schema{Type: schema.TypeBool, Optional: true},
			"enable":  &schema.Schema{Type: schema.TypeBool, Optional: true},
			"command": &schema.Schema{Type: schema.TypeString, Optional: true},
			"mask":    &schema.Schema{Type: schema.TypeBool, Optional: true},
			"dropin": &schema.Schema{
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
