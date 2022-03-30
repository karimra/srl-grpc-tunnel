# srl-grpc-tunnel

SRL gRPC Tunnel is an [SR Linux](https://learn.srlinux.dev/) [NDK](https://learn.srlinux.dev/ndk/intro/) application that adds support for [Openconfig gRPC tunnel](https://github.com/openconfig/grpctunnel) to SR Linux.

It acts as a gRPC tunnel client to allow access to locally configured targets (gNMI server, gNMI server, SSH server...)

It connects to a gRPC tunnel server such as [gNMIc](https://gnmic.kmrd.dev/user_guide/tunnel_server/).

## Features

* gRPC tunnel client handling gNMI, SSH and custom targets

* Both secure and insecure connections are supported

* Multiple destinations per tunnel (all active)

* Multiple targets per tunnel

* Predefined target ID (node-name, user-agent, mac-address)

* Predefined target type (gNMI server, SSH server)

* Custom target ID and Type, either a user defined string or a Go template.

## Installation

Download the pre build RPM file from the repo's [release page](https://github.com/karimra/srl-grpc-tunnel/releases), or run:

```bash
wget https://github.com/karimra/srl-grpc-tunnel/releases/download/v0.0.1/srl-grpc-tunnel_0.0.1_Linux_x86_64.rpm
```

Copy the RPM file to your SR Linux instance and run (from bash)

```bash
sudo rpm -i srl-grpc-tunnel_0.0.1_Linux_x86_64.rpm
```

Start an `sr_cli` session and reload the application manager

```bash
--{ + running }--[  ]--                                                                                                                                              
A:srl1# tools system app-management application app_mgr reload
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

``` bash
--{ + running }--[ system grpc-tunnel ]--                                                 
A:srl1#                                                                                   
Local commands:
  admin-state*      Administrative state of the gRPC tunnel application
  destination       list of gRPC tunnel destinations, i.e gRPC tunnel servers
  tunnel            gRPC tunnel(s)
```

### Destinations (gRPC Tunnel servers)

```bash
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

```bash
enter candidate
# create destination d1
/ system grpc-tunnel destination d1 
# set an address and a port number for d1
/ system grpc-tunnel destination d1 address 172.20.20.2 port 57405 # (default port 57401)
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

```bash
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

```bash
enter candidate
# create tunnel d1
/ system grpc-tunnel tunnel t1
# link destination d1 to tunnel t1
/ system grpc-tunnel tunnel t1 destination d1
commit now
```

#### gNMI

```bash
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

Add a target called tg1, type `grpc-server` with ID `node-name`
#### CLI

```bash
enter candidate
# add target with type `grpc-server`
/ system grpc-tunnel tunnel t1 target tg1 type grpc-server 
# set target ID to `node-name`
/ system grpc-tunnel tunnel t1 target tg1 id node-name
commit now
```

#### gNMI

```bash
gnmic -a clab-grpc-tunnel-srl1 -u admin -p admin --skip-verify set \
    --update /system/grpc-tunnel/tunnel[name=t1]/target[name=tg1]/type/grpc-server:::json_ietf:::'[null]' \
    --update /system/grpc-tunnel/tunnel[name=t1]/target[name=tg1]/id/node-name:::json_ietf:::'[null]'
```

### Enable Tunnel

#### CLI

```bash
enter candidate
# enable tunnel t1
/ system grpc-tunnel tunnel t1 admin-state enable
commit now
```

#### gNMI

```bash
gnmic -a clab-grpc-tunnel-srl1 -u admin -p admin --skip-verify set \
    --update /system/grpc-tunnel/tunnel[name=t1]/admin-state:::json_ietf:::enable \
```
