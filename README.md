# srl-grpc-tunnel

SRL gRPC Tunnel is an [SR Linux](https://learn.srlinux.dev/) [NDK](https://learn.srlinux.dev/ndk/intro/) application that adds support for [Openconfig gRPC tunnel](https://github.com/openconfig/grpctunnel) to SR Linux.

It acts as a gRPC tunnel client to allow access to locally configured targets (gNMI server, gNOI server, SSH server...)

It connects to a gRPC tunnel server such as [gNMIc](https://gnmic.openconfig.net/user_guide/tunnel_server/).

## Features

* gRPC tunnel client handles gNMI, SSH and custom targets

* Both secure and insecure connections are supported

* Multiple destinations per tunnel (all active)

* Multiple targets per tunnel

* Predefined target ID (node-name, user-agent, mac-address)

* Predefined target type (gNMI server, SSH server)

* Custom target ID and Type, either a user defined string or a Go template.

## Installation

### Automated install with lab

For an automated lab deployment with 2 SR Linux, 2 gNMIc (tunnel server), a Prometheus server and a consul server see [here](https://github.com/karimra/srl-grpc-tunnel/tree/main/example#readme)

### Manual installation

Download the pre build .deb file from the repo's [release page](https://github.com/karimra/srl-grpc-tunnel/releases), or run:

```bash
version=0.1.8
wget https://github.com/karimra/srl-grpc-tunnel/releases/download/v${version}/srl-grpc-tunnel_${version}_Linux_x86_64.deb
```

Copy the .deb file to your SR Linux instance and run (from bash)

```bash
sudo dpkg -i srl-grpc-tunnel_${version}_Linux_x86_64.deb
```

Reload the application manager

```bash
sr_cli tools system app-management application app_mgr reload
```

Check that the `grpc-tunnel` application is running

```bash
--{ + running }--[  ]--                                                                   
A:srl1# show system application grpc-tunnel                                               
  +-------------+------+---------+----------------+--------------------------+
  |    Name     | PID  |  State  |    Version     |       Last Change        |
  +=============+======+=========+================+==========================+
  | grpc-tunnel | 2837 | running | v0.1.0-42f4b74 | 2022-03-30T05:21:57.649Z |
  +-------------+------+---------+----------------+--------------------------+
--{ + running }--[  ]--                                                                   
A:srl1#     
```

## Configuration

This application relies on SRL's gNMI server unix socket to serve sessions towards GNMI_GNOI targets. It should be enabled before starting the grpc-tunnel application.

```shell
enter candidate
# enable the gnmi-server unix-socket
/ system gnmi-server unix-socket admin-state enable
# disable authentication (optional)
/ system gnmi-server unix-socket use-authentication false
# enable gNMI and gNOI (optional) services
/ system gnmi-server unix-socket services [ gnmi gnoi ]
# commit
commit now
```

The application configuration has 3 sections; its `admin-state`, the list of `destinations` and the `tunnels`

``` shell
--{ + running }--[ system grpc-tunnel ]--                                                 
A:srl1#                                                                                   
Local commands:
  admin-state*      Administrative state of the gRPC tunnel application
  destination       list of gRPC tunnel destinations, i.e gRPC tunnel servers
  tunnel            gRPC tunnel(s)
```

### Destinations (gRPC Tunnel servers)

```shell
--{ + running }--[ system grpc-tunnel ]--                                                 
A:srl1# destination d1                                                                    
usage: destination <name>

list of gRPC tunnel destinations, i.e gRPC tunnel servers

Positional arguments:
  name              [string] destination name

Local commands:
  address*          destination address
  description*      destination description
  network-instance*
                    Reference to a configured network-instance used to establish the gRPC
                    tunnel from.
  no-tls*           when true the connection to this destination will be insecure
  port*             destination port number
  tls-profile*      NOT IMPLEMENTED: Reference to the TLS profile to use when initiating
                    connections to this destination.
```

Setting a tls-profile on the client side is not implemented yet, by default the tunnel client will use a one-way TLS connection. TLS can be disabled by setting `no-tls true`

#### CLI

To configure a gRPC tunnel destination `d1` run the below commands:

```shell
enter candidate
# create destination d1
/ system grpc-tunnel destination d1 
# set an address and a port number for d1
/ system grpc-tunnel destination d1 address 172.20.20.2 port 57401 # (default port 57401)
# use a non secure connection for d1
/ system grpc-tunnel destination d1 no-tls true
# use network-instance mgmt to connect to destination d1 (default: mgmt)
/ system grpc-tunnel destination d1 network-instance mgmt # default
commit now
```

```text
--{ + running }--[ system grpc-tunnel destination d1 ]--                                  
A:srl1# info                                                                              
    address 172.20.20.2
    port 57401
    network-instance mgmt
    no-tls true
--{ + running }--[ system grpc-tunnel destination d1 ]--                                  
A:srl1#
```

#### gNMI

```shell
gnmic -a clab-grpc-tunnel-srl1 -u admin -p admin --skip-verify set \
    --update /system/grpc-tunnel/destination[name=d1]/address:::json_ietf:::172.20.20.2 \
    --update /system/grpc-tunnel/destination[name=d1]/port:::json_ietf:::57401 \
    --update /system/grpc-tunnel/destination[name=d1]/network-instance:::json_ietf:::mgmt \
    --update /system/grpc-tunnel/destination[name=d1]/no-tls:::json_ietf:::true
```

### Tunnel (gRPC Tunnel)

Create a Tunnel `t1` and link the destination `d1` to it.

A max of 16 tunnels can be created, To each one, 16 destinations can be linked.

#### CLI

To configure a gRPC tunnel `t1` run the below commands:

```shell
enter candidate
# create tunnel d1
/ system grpc-tunnel tunnel t1
# link destination d1 to tunnel t1
/ system grpc-tunnel tunnel t1 destination d1
commit now
```

#### gNMI

```shell
gnmic -a clab-grpc-tunnel-srl1 -u admin -p admin --skip-verify set \
    --update /system/grpc-tunnel/tunnel[name=t1]:::json_ietf:::'{"destination":{"name":"d1"}}'
```

### Targets

```text
--{ + running }--[ system grpc-tunnel tunnel t1 ]--                                       
A:srl1# target tg1                                                                        
usage: target <name>

Positional arguments:
  name              [string, length 1..256] target local name

Local commands:
  id                target ID
  local-address*    local address to dial for an established tunnel towards this target
  type              target type
```

The `id` and `type` are the unique identifiers used to register the target with the gRPC tunnel server.
The `local-address` is used to customize the local handler behavior by changing the dialed address when a request is received through the tunnel.

The target `id` values are:

* `node-name`: The configure node host-name under `system name host-name`
* `user-agent`: A custom string in the format `<node-name>:nokia-srl:<chassis>:<sw-version>`
* `mac-address`: The node chassis mac address
* `custom <string>`: A user defined string, or a Go template that uses the systemInfo struct as input.

The target `type` values are:

* `grpc-server`: This sets the target type to `GNMI_GNOI` when registering the target with the gRPC tunnel server. In this case, the `local-address` defaults to `unix:///opt/srlinux/var/run/sr_gnmi_server`.
* `ssh-server`: This sets the target type to `SSH` when registering the target with the gRPC tunnel server. In this case, the `local-address` defaults to `localhost:22`.
* `custom`: Sets a custom string or Go template as the target `type`. In this case setting the `local-address` is mandatory.

E.g: Add a target called tg1, type `grpc-server` with ID `node-name`

#### CLI

```shell
enter candidate
# add target with type `grpc-server`
/ system grpc-tunnel tunnel t1 target tg1 type grpc-server 
# set target ID to `node-name`
/ system grpc-tunnel tunnel t1 target tg1 id node-name
commit now
```

#### gNMI

```shell
gnmic -a clab-grpc-tunnel-srl1 -u admin -p admin --skip-verify set \
    --update /system/grpc-tunnel/tunnel[name=t1]/target[name=tg1]/type/grpc-server:::json_ietf:::'[null]' \
    --update /system/grpc-tunnel/tunnel[name=t1]/target[name=tg1]/id/node-name:::json_ietf:::'[null]'
```

### Enable Tunnel

#### CLI

```shell
enter candidate
# enable tunnel t1
/ system grpc-tunnel tunnel t1 admin-state enable
commit now
```

#### gNMI

```bash
gnmic -a clab-grpc-tunnel-srl1 -u admin -p admin --skip-verify set \
    --update /system/grpc-tunnel/tunnel[name=t1]/admin-state:::json_ietf:::enable
```

## State

```text
--{ + running }--[ system grpc-tunnel ]--                                                 
A:srl1# info from state                                                                   
    admin-state enable
    oper-state up
    destination d1 {
        address 172.20.20.2
        port 57401
        network-instance mgmt
        no-tls true
    }
    tunnel t1 {
        admin-state enable
        oper-state up
        oper-state-down-reason ""
        destination d1 {
            oper-state up
            oper-state-down-reason ""
            target srl1 type GNMI_GNOI {
                oper-state up
                oper-state-down-reason ""
            }
        }
        target tg1 {
            id {
                node-name
            }
            type {
                grpc-server
            }
        }
    }
--{ + running }--[ system grpc-tunnel ]--   
```
