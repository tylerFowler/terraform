package template

import (
	"testing"

	r "github.com/hashicorp/terraform/helper/resource"
	"github.com/hashicorp/terraform/helper/schema"
)

func TestCoreosCloudinitRender(t *testing.T) {
	testCases := []struct {
		ResourceBlock string
		ExpectedOut   string
	}{
		{ // empty block case
			`data "template_coreos_cloudinit" "test" {}`,
			"#cloud-config\ncoreos:\n",
		},
		{ // test systemd unit output
			`data "template_coreos_cloudinit" "test" {
				systemd_unit {
					name = "tst.mount"
					command = "start"
					content = <<EOF
[Unit]
Description=Test Mount
Requires=dev-xvde.device
After=dev-xvde.device

[Mount]
What=/dev/xvde
Where=/tst
Type=ext4
EOF
				}

				systemd_unit {
					name = "test.service"
					runtime = true
					mask = true
					command = "start"
					enable = true
					content = <<EOF
[Unit]
Description=A test service

[Service]
Restart=on-failure
RestartSec=15

ExecStartPre=-/usr/sbin/stat /some-file
ExecStart=/usr/bin/cat /some-file

[Install]
WantedBy=multi-user.target
EOF

					dropin {
						name = "10-test-dropin.conf"
						content = "I'm a dropin"
					}

					dropin {
						name = "20-other-one.conf"
						content = "Another dropin"
					}
				}
			}`,
			"#cloud-config\ncoreos:\n  units:\n    - name: tst.mount\n      command: start\n      content: |\n        [Unit]\n        Description=Test Mount\n        Requires=dev-xvde.device\n        After=dev-xvde.device\n\n        [Mount]\n        What=/dev/xvde\n        Where=/tst\n        Type=ext4\n    - name: test.service\n      runtime: true\n      mask: true\n      enable: true\n      command: start\n      content: |\n        [Unit]\n        Description=A test service\n\n        [Service]\n        Restart=on-failure\n        RestartSec=15\n\n        ExecStartPre=-/usr/sbin/stat /some-file\n        ExecStart=/usr/bin/cat /some-file\n\n        [Install]\n        WantedBy=multi-user.target\n      drop-ins:\n        - name: 10-test-dropin.conf\n          content: |\n            I'm a dropin\n        - name: 20-other-one.conf\n          content: |\n            Another dropin\n",
		},
		{ // should allow empty content systemd units iff we're using dropins
			`data "template_coreos_cloudinit" "test" {
				systemd_unit {
					name = "etcd2.service"
					dropin {
						name = "10-etcd-name.conf"
						content = <<EOF
[Service]
Environment="ETCD_NAME=node_001"
EOF
					}
				}
			}`,
			"#cloud-config\ncoreos:\n  units:\n    - name: etcd2.service\n      drop-ins:\n        - name: 10-etcd-name.conf\n          content: |\n            [Service]\n            Environment=\"ETCD_NAME=node_001\"\n",
		},
		{ // write_file blocks
			`data "template_coreos_cloudinit" "test" {
				write_file {
					path = "~/hello_world"
					permissions = "0644"
					owner = "core:core"
					encoding = "utf8"

					content = "hello world"
				}
			}`,
			"#cloud-config\ncoreos:\nwrite_files:\n  - path: \"~/hello_world\"\n    permissions: 0644\n    owner: core:core\n    encoding: utf8\n    content: |\n      hello world\n",
		},
		{ // use_shebang
			`data "template_coreos_cloudinit" "test" {
				use_shebang = true
			}`,
			"#!cloud-config\ncoreos:\n",
		},
		{ // top level values (hostname, manage_etc_hosts, ssh_authorized_keys)
			`data "template_coreos_cloudinit" "test" {
				use_shebang = false
				hostname = "some-host"
				ssh_authorized_keys = [ "ssh-rsa apublickey", "ssh-rsa anotherpublickey" ]
				manage_etc_hosts = "127.0.0.1"
			}`,
			"#cloud-config\nhostname: some-host\nssh_authorized_keys:\n  - \"ssh-rsa apublickey\"\n  - \"ssh-rsa anotherpublickey\"\nmanage_etc_hosts: 127.0.0.1\ncoreos:\n",
		},
		{ // coreos.* values (fleet, etcd2, etc...), can only do one key per block since the order is indeterminant
			`data "template_coreos_cloudinit" "test" {
				etcd {
					name = "node001"
				}

				etcd2 {
					initial_advertise_peer_urls = "http://$private_ipv4:2380"
				}

				fleet {
					engine_reconcile_interval = 30
				}

				flannel {
					etcd_endpoints = "https://127.0.0.1:2379,https://$private_ipv4:2379"
				}

				locksmith {
					endpoint = "https://etcd.example.com:2379"
				}

				update_strategy {
					reboot_strategy = "best-effort"
				}
			}`,
			"#cloud-config\ncoreos:\n  etcd:\n    name: \"node001\"\n  etcd2:\n    initial-advertise-peer-urls: \"http://$private_ipv4:2380\"\n  fleet:\n    engine-reconcile-interval: 30\n  flannel:\n    etcd-endpoints: \"https://127.0.0.1:2379,https://$private_ipv4:2379\"\n  locksmith:\n    endpoint: \"https://etcd.example.com:2379\"\n  update:\n    reboot-strategy: \"best-effort\"\n",
		},
	}

	for _, tt := range testCases {
		r.UnitTest(t, r.TestCase{
			Providers: testProviders,
			Steps: []r.TestStep{
				r.TestStep{
					Config: tt.ResourceBlock,
					Check: r.ComposeTestCheckFunc(
						r.TestCheckResourceAttr("data.template_coreos_cloudinit.test", "rendered", tt.ExpectedOut),
					),
				},
			},
		})
	}
}

// Validation Func Tests
func TestEtcHostsValidation(t *testing.T) {
	testCases := []struct {
		EtcHost        string
		ExpectsWarning bool
	}{
		{EtcHost: "localhost", ExpectsWarning: false},
		{EtcHost: "", ExpectsWarning: true},
		{EtcHost: "10.0.0.15", ExpectsWarning: true},
	}

	for _, tc := range testCases {
		warnings, _ := etcHostsValidation(tc.EtcHost, "")

		if tc.ExpectsWarning && len(warnings) == 0 {
			t.Errorf("Expected warning to be given for host %v but none was given", tc.EtcHost)
		} else if !tc.ExpectsWarning && len(warnings) > 0 {
			t.Errorf("Expected no warning to be given for host %v but was given warnings %v", tc.EtcHost, warnings)
		}
	}
}

func TestLocksmithRebootStrategy(t *testing.T) {
	testCases := []struct {
		RebootStrategy string
		ExpectsError   bool
	}{
		{RebootStrategy: "reboot", ExpectsError: false},
		{RebootStrategy: "etcd-lock", ExpectsError: false},
		{RebootStrategy: "best-effort", ExpectsError: false},
		{RebootStrategy: "off", ExpectsError: false},
		{RebootStrategy: "", ExpectsError: true},
		{RebootStrategy: "not-a-strategy", ExpectsError: true},
	}

	for _, tc := range testCases {
		_, errors := updateSchema.Elem.(*schema.Resource).Schema["reboot_strategy"].ValidateFunc(tc.RebootStrategy, "")

		if tc.ExpectsError && len(errors) == 0 {
			t.Errorf("Expected error to be given for reboot strategy %v but none was given", tc.RebootStrategy)
		} else if !tc.ExpectsError && len(errors) > 0 {
			t.Errorf("Expected no error to be given for reboot strategy %v but was given errors %v", tc.RebootStrategy, errors)
		}
	}
}

// Utility Func Tests
func TestIndentString(t *testing.T) {
	testCases := []struct {
		IndentLvl  int
		Unindented string
		Indented   string
	}{
		{IndentLvl: 2, Unindented: "some text", Indented: "\t\tsome text\n"},
		{IndentLvl: 2, Unindented: "some\ntext", Indented: "\t\tsome\n\t\ttext\n"},
		{IndentLvl: 0, Unindented: "some text", Indented: "some text\n"},
		// this one should not add tabs to the empty line
		{IndentLvl: 2, Unindented: "some text\n\nmore text", Indented: "\t\tsome text\n\n\t\tmore text\n"},
		{IndentLvl: 4, Unindented: "", Indented: ""},
	}

	for _, tc := range testCases {
		indented := indentString(tc.IndentLvl, &tc.Unindented)
		if indented != tc.Indented {
			t.Errorf("Expected indented string to be %q but got %q", tc.Indented, indented)
		}
	}
}
