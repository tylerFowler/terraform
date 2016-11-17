---
layout: "template"
page_title: "Template: coreos_cloudinit"
sidebar_current: "docs-template-datasource-coreos-cloudinit"
description: |-
  Renders a [CoreOS cloud-config](https://coreos.com/os/docs/latest/cloud-config.html) from configuration blocks
---

# template\_coreos\_cloudinit

Renders a [CoreOS cloud-config](https://coreos.com/os/docs/latest/cloud-config.html) from configuration blocks

## Example Usage

```hcl
variable hostname { default = "node_001" }

# Load a systemd service definition using a `template_file`
data "template_file" "nginx_service" {
  template = "${file("${path.module}/nginx.service")}"
  vars {
    hostname = "${var.hostname}"
  }
}

# Render a CoreOS cloud config with fleet enabled and our
# service
data "template_coreos_cloudinit" "cloud_config" {
  hostname = "${var.hostname}"

  fleet {
    public_ip = "$public_ipv4"
  }

  # enable fleet systemd service, and use a dropin to set the
  # metadata env variable (could also use the `metadata` key in
  # the fleet configuration block)
  systemd_unit {
    name = "fleet.service"
    command = "start"
    enable = true

    dropin {
      name = "10-metadata.conf"
      content = <<EOF
[Service]
Environment="FLEET_METADATA=region=us-west-2"
EOF
    }
  }

  # enable our nginx service from the template file
  systemd_unit {
    name = "nginx.service"
    command = "start"
    content = "${data.template_file.nginx_service.rendered}"
  }

  # write a file
  write_file {
    path = "/etc/resolv.conf"
    permissions = "0644"
    owner = "root"
    content = "nameserver 8.8.8.8"
  }
}

# now use our cloud-config in an AWS instance
resource "aws_instance" "appserver" {
  ami = "my-ami1234"
  instance_type = "t2.micro"
  user_data = "${data.template_coreos_cloudinit.cloud_config.rendered}"
}
```

## Argument Reference

-> *Note:* In a lot of cases the arguments that can be given map directly to CoreOS values with the names converted from underscores (`some_val`) to dashes (`some-val`) when rendered to the cloudinit file. As such, more documentation on each configuration parameter can be found on the CoreOS docs site.

All arguments are optional, since this provider will only write the keys that are defined an empty configuration results in a cloudinit file that looks like `#cloud-config\n`. Also note that since this simply provides a plaintext string of the final cloud-config file in YAML format, substituted values supported by many cloud providers (i.e. the `$private_ipv4` or `$public_ipv4`) can be used to create dynamic cloud_configs.

The follow arguments are supported:

- `use_shebang` - (Optional) Whether or not to use the shebang (`#` vs `#!`) in the `#cloud-config` header directive.
Defaults to `False`.
- `hostname` - (Optional) Sets the hostname key in the cloud-config.
- `ssh_authorized_keys` - (Optional) List of public SSH keys which will be authorized for the `core` user.
- `manage_etc_hosts` - (Optional) Configures contents of `/etc/hosts`, CoreOS currently *only* supports the value "localhost", which is enforced by this Terraform provider.
- `user` - (Optional) Block that specifies a user to be added to the cloudinit's `users` key, can be used many times. Maintains the order the blocks are specified in, however the keys in the user entry will not be kept in order.
- `update_strategy` - (Optional) Block specifying the update strategy to be used along with any extra configuration supported by CoreOS, can only be specified once.
- `etcd` - (Optional) Block specifying an etcd configuration using the keys provided by CoreOS, can only be specified once. Deprecated by CoreOS in favor of the `etcd2` block.
- `etcd2` - (Optional) Block specifying an etcd2 configuration using the keys provided by CoreOS, can only be specified once.
- `fleet` - (Optional) Block specifying a fleet configuration using the keys provided by CoreOS, can only be specified once.
- `flannel` - (Optional) Block specifying a flannel configuration using the keys provided by CoreOS, can only be specified once.
- `locksmith` - (Optional) Block specifying a locksmith configuration using the keys provided by CoreOS, can only be specified once.
- `systemd_unit` - (Optional) Block that adds a unit to the `coreos.units.*` list, can be used as many times as necessary. The unit blocks will be written to the cloudinit in the same order as they appear in the Terraform configuration. If none are given the `coreos.units` key will not be written.
- `write_file` - (Optional) Block that adds a file definition to the `write_files.*` list, can be used as many times as necessary. The write file blocks will be written to the cloutinit in the same order as they appear in the Terraform configuration. If none are given the `write_files` key will not be written.

## Systemd Unit Configuration
The `systemd_unit` blocks allow users to add arbitrary Systemd unit files to the cloudinit. This is done by passing any string into the `content` key, which can be an inline string or interpolated from a file or even a `template_file` resource. This block also allows users to add dropins to their services (or system services), or just enable systemd services already on the system. The keys will be briefly explained here, but you can see the [CoreOS cloud-config doc](https://coreos.com/os/docs/latest/cloud-config.html#units) for more details on each key.

-> *Note:* currently, as a part of the cloud-config building functionality to keep consistent indentation in the resulting YAML file, all tab escape characters (`\t`) in the content will be translated to two spaces.

The `systemd_unit` block supports the following:

- `name` - (Required) String representing the name of the unit (i.e. `nginx.service`).
- `content` - String containing the contents of the Systemd unit, required if there are no child `dropin` blocks or if there is no `command` value.
- `runtime` - Boolean indicating whether to persist the unit across reboots.
- `enable` - Boolean indicating whether to use the unit's `[Install]` section.
- `command` - Command to execute on the unit, see CoreOS docs (link above) for possible values. Required if the unit has no content and no dropins.
- `mask` - Boolean indicating whether or not to mask the unit file.
- `dropin` - A block specifying a dropin, required if no content or command values were given. Supports the following arguments:
  - `name` - (Required) Name of the dropin (i.e. `10-tls-verify.conf`)
  - `content` - (Required) String containing the contents of the dropin.

## Write File Configuration
The `write_file` blocks allow users to write arbitrary items to the `write_files` section of the cloud-config, so that the files will make it onto the machine provisioned with the cloud-config. Similar to the `systemd_unit` block, the `content` key can be populated from sources like interpolated files, raw strings, or even other rendered `template_file` datasources. The keys will be briefly explained here, but you can see the [CoreOS cloud-config doc](https://coreos.com/os/docs/latest/cloud-config.html#writefiles) for more details on each key.

-> *Note:* currently, as a part of the cloud-config building functionality to keep consistent indentation in the resulting YAML file, all tab escape characters (`\t`) in the content will be translated to two spaces.

The `write_file` block supports the following:

- `path` - (Required) The absolute location on disk where the contents will be written.
- `content` - (Required) The data that will be written to the file.
- `permissions` - String containing a permissions integer (i.e. `0644`).
- `owner` - String containing a user or user and group (i.e. `user:group`).
- `encoding` - Encoding of the data passed to the content key. See the CoreOS docs (linked above) for supported values.

## User Configuration
The `user` blocks will add a user definition to the cloudinit as a list item under the `users` key. This will add or modify existing users to the machine, see the [CoreOS users doc](https://coreos.com/os/docs/latest/cloud-config.html#users) for more information on each option. The users added to the cloudinit will be added in the order they are listed in the source Terraform configuration, although the user keys in each item will not necessarily be in the same order.

The `user` block supports the following:

- `name` - Login name of the user.
- `gecos` - (Optional) GECOS comment of the user.
- `passwd` - (Optional) Hash of the password to use for this user.
- `homedir` - (Optional) The user's home directory, CoreOS defaults to `/home/<name>` if not specified.
- `no_create_home` - (Optional) Boolean, skips home directory creation if true. Defaults to `false`.
- `primary_group` - (Optional) Default group for the user.
- `no_user_group` - (Optional) Boolean, skips the default group creation. Defaults to `false`.
- `coreos_ssh_import_github` - (Optional, Deprecated) Authorize SSH keys from GitHub user.
- `coreos_ssh_import_github_users` - (Optional, Deprecated) Authorize keys from a lit of GitHub users. The Terraform type for this option is a simple string and so should be preformatted to fit the CoreOS cloudinit format.
- `coreos_ssh_import_url` - (Optional, Deprecated) Authorize SSH keys imported from a URL endpoint.
- `system` - (Optional) Boolean, creates the user as a system user if true, skipping home directory creation. Defaults to `false`.
- `no_log_init` - (Optional) Boolean, skips initialization of lastlog and failog databases if true. Defaults to `false`.
- `shell` - (Optional) The user's login shell.
- `groups` - (Optional) A list of strings that are group names the user will be added to.
- `ssh_authorized_keys` - (Optional) A list of public SSH keys to authorize for the user.

## CoreOS Config Blocks
The CoreOS keys (i.e. the keys in the config file under `coreos.*`) map directly to a configuration block in Terraform usually with the same name. The keys used for each block in this provider map directly to the native CoreOS configuration keys, the only difference being that this provider will have keys using underscores (i.e. `some_key`) that map to the CoreOS keys using dashes (i.e. `some-key`), which themselves typically map to environment variables used to configure it's various native services.

### Etcd Configuration
-> *Note:* The `etcd` key has been deprecated by CoreOS in favor the `etcd2` configuration, as such this provider will emit a deprecation warning when using `etcd`.

The etcd configuration block allows parameters that map directly to etcd configuration flags, please refer to the CoreOS [etcd configuration](https://github.com/coreos/etcd/blob/release-0.4/Documentation/configuration.md) doc to find explanations for these flags as they will not be explained here.

The `etcd` block supports the following, all are optional unless specified otherwise:

- `name` - (Required) Name of the node.
- `addr` - String containing a `hostname:port`.
- `discovery` - String containing an etcd discovery URL such as `https://discovery.etcd.io/your-token`
- `http_read_timeout` - Integer timeout value.
- `http_write_timeout` - Integer timeout value.
- `bind_addr` - String containing a `hostname:port`.
- `peers` - String containing a comma separated list of addresses & ports.
- `ca_file` - String containing a file path.
- `cert_file` - String containing a file path.
- `key_file` - String containing a file path.
- `cors` - String containing a comma separated list of CORS origins.
- `data_dir` - String containing a path to a directory.
- `max_result_buffer` - Integer value.
- `max_retry_attempts` - Integer value.
- `peer_addr` - String containing a `hostname:port`.
- `peer_bind_addr` - String containing a `hostname:port`.
- `peer_ca_file` - String containing a file path.
- `peer_cert_file` - String containing a file path.
- `peer_key_file` - String containing a file path.
- `peer_election_timeout` - Integer timeout value.
- `peer_heartbeat_interval` - Integer value.
- `snapshot` - Boolean value, doesn't default to anything when not set.
- `cluster_active_size` - Integer value.
- `cluster_remove_delay` - Integer value.
- `cluster_sync_interval` - Integer value.

### Etcd2 Configuration
The etcd2 configuration block allows parameters that map directly to etcd2 configuration flags, please refer to the CoreOS [etcd2 configuration](https://github.com/coreos/etcd/blob/v2.3.2/Documentation/configuration.md) doc to find explanations for these flags as they will not be explained here.

The `etcd2` block supports the following, all are optional unless specified otherwise:

- `name` - String value.
- `data_dir` - String containing a file path.
- `wal_dir` - String containing a file path.
- `snapshot_count` - Integer value.
- `heartbeat_interval` - Integer value.
- `election_timeout` - Integer timeout value.
- `listen_peer_urls` - String containing a comma separated list of urls with ports.
- `listen_client_urls` - String containing a comma separated list of urls with ports.
- `max_snapshots` - Integer value.
- `max_wals` - Integer value.
- `cors` - String containing a comma separated list of CORS origins.
- `initial_advertise_peer_urls` - String containing a comma separated list of urls with ports.
- `initial_cluster` - String containing a comma separated list of urls with ports and a nodename. (i.e. `node1=http://10.0.0.1:2379,node2=http://10.0.0.2:2379`).
- `initial_cluster_state` - String containing one of `"new"` or `"existing"`.
- `initial_cluster_token` - String containing a cluster token.
- `advertise_client_urls` - String containing a comma separated list of urls with ports.
- `discovery` - String containing an etcd discovery URL.
- `discovery_srv` - String value.
- `discovery_fallback` - String containing one of `"exit"` or `"proxy"`.
- `discovery_proxy` - String containing a URL.
- `strict_reconfig_check` - Boolean value.
- `proxy` - String containing one of `"off"`, `"readonly"`, or `"on"`.
- `proxy_failure_wait` - Integer timeout value.
- `proxy_refresh_interval` - Integer value.
- `proxy_dial_timeout` - Integer timeout value.
- `proxy_write_timeout` - Integer timeout value.
- `proxy_read_timeout` - Integer timeout value.
- `ca_file` - String containing a file path.
- `cert_file` - String containing a file path.
- `key_file` - String containing a file path.
- `client_cert_auth` - Boolean value.
- `trusted_ca_file` - String containing a file path.
- `peer_ca_file` - String containing a file path.
- `peer_cert_file` - String containing a file path.
- `peer_key_file` - String containing a file path.
- `peer_client_cert_auth` - Boolean value.
- `peer_trusted_ca_file` - String containing a file path.
- `debug` - Boolean value.
- `log_package_levels`
- `force_new_cluster` - Boolean value.
- `enable_pprof` - Boolean value.

### Fleet Configuration
The fleet configuration block allows parameters that map directly to fleet configuration flags, please refer to the CoreOS [cloud-config doc](https://coreos.com/os/docs/latest/cloud-config.html#fleet) doc to find explanations for these flags as they will not be explained here.

The `fleet` block supports the following, all are optional unless specified otherwise:

- `public_ip` - String containing an IP address.
- `agent_ttl` - Integer timeout value.
- `engine_reconcile_interval` - Integer value.
- `metadata` - String containing a comma separated list of key/value pairs delimited by `=` characters (i.e. `key1=val1,key2=val2`).
- `verbosity` - Integer value, enabled by setting this to a value greater than zero.
- `etcd_cafile` - String containing a file path.
- `etcd_certfile` - String containing a file path.
- `etcd_keyfile` - String containing a file path.
- `etcd_key_prefix` - String value.
- `etcd_request_timeout` - Integer timeout value.
- `etcd_servers` - String containing a comma separated list of etcd endpoints (i.e. `http://127.0.0.1:2379,https://$private_ipv4:2379`).

### Flannel Configuration
The flannel configuration block allows parameters that map directly to flannel configuration flags, please refer to the CoreOS [cloud-config doc](https://coreos.com/os/docs/latest/cloud-config.html#flannel) doc to find explanations for these flags as they will not be explained here.

The `flannel` block supports the following, all are optional unless specified otherwise:

- `ip_masq` - Boolean value
- `subnet_file` - String containing a file path.
- `interface` - String containing a name or IP.
- `public_ip` - String containing an IP address.
- `etcd_endpoints` - String containing a comma separated list of etcd endpoints (i.e. `http://127.0.0.1:2379,https://$private_ipv4:2379`).
- `etcd_cafile` - String containing a file path.
- `etcd_certfile` - String containing a file path.
- `etcd_keyfile` - String containing a file path.
- `etcd_prefix` - String value.

### Locksmith Configuration
The locksmith configuration block allows parameters that map directly to locksmith configuration flags, please refer to the CoreOS [cloud-config doc](https://coreos.com/os/docs/latest/cloud-config.html#locksmith) doc to find explanations for these flags as they will not be explained here.

The `locksmith` block supports the following, all are optional unless specified otherwise:

- `endpoint` - String containing a comma separated list of etcd endpoints (i.e. `http://127.0.0.1:2379,https://$private_ipv4:2379`).
- `etcd_cafile` - String containing a file path.
- `etcd_certfile` - String containing a file path.
- `etcd_keyfile` - String containing a file path.
- `group` - String value.
- `window_start` - String value indicating a time.
- `window_length` - Integer value.

### Update Strategy Configuration
The update_strategy configuration block allows parameters that map directly to update configuration flags, please refer to the CoreOS [cloud-config doc](https://coreos.com/os/docs/latest/cloud-config.html#update) doc to find explanations for these flags as they will not be explained here.

The `update_strategy` block supports the following, all are optional unless specified otherwise:

- `reboot_strategy` - String containing one of `"reboot"`, `"etcd-lock"`, `"best-effort"`, or `"off"`.
- `server` - String containing a server address.
- `group` - String containing one of `"master"`, `"alpha"`, `"beta"`, or `"stable"`.

## Attributes Reference

The following attributes are supported:

- `rendered` - The final cloud-config in plaintext.
