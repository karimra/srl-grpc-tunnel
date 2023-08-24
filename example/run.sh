#!/bin/bash

# download the RPM file

wget -qL https://github.com/karimra/srl-grpc-tunnel/releases/download/v0.0.1/srl-grpc-tunnel_0.1.5_Linux_x86_64.rpm -P rpm/

#deploy the lab
sudo clab deploy -t grpc_tunnel.clab.yaml --reconfigure 
username=admin
password=NokiaSrl1!

# enable gnmi unix-socket
gnmic -u $username -p $password -a clab-grpc-tunnel-srl1,clab-grpc-tunnel-srl2 --skip-verify -e json_ietf set \
    --update-path /system/gnmi-server/unix-socket/admin-state \
    --update-path /system/gnmi-server/unix-socket/use-authentication \
    --update-value enable \
    --update-value true

# ACLs
gnmic -u $username -p $password -a clab-grpc-tunnel-srl1,clab-grpc-tunnel-srl2 --skip-verify -e json_ietf set \
    --request-file acls.yaml

# install the RPM located in /tmp/rpm
sudo clab exec --topo grpc_tunnel.clab.yaml --label clab-node-kind=srl --label containerlab=grpc-tunnel --cmd "sudo rpm -U /tmp/rpm/*rpm"

# reload the app manager so it picks up the newly installed app
sudo clab exec --topo grpc_tunnel.clab.yaml --label clab-node-kind=srl --label containerlab=grpc-tunnel --cmd "sr_cli tools system app-management application app_mgr reload"

# check the app status in both SRLs
sudo clab exec --topo grpc_tunnel.clab.yaml --label clab-node-kind=srl --label containerlab=grpc-tunnel --cmd "sr_cli show system application grpc-tunnel"

# get gNMIc IPs
gnmic1_ip=$(docker inspect clab-grpc-tunnel-gnmic1 -f '{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}')
gnmic2_ip=$(docker inspect clab-grpc-tunnel-gnmic2 -f '{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}')

# create the vars file
echo "Destination1: $gnmic1_ip" > config_grpc_tunnel_vars.yaml
echo "Destination2: $gnmic2_ip" >> config_grpc_tunnel_vars.yaml

# configure both SRL1 and SRL2
gnmic -u $username -p $password -a clab-grpc-tunnel-srl1,clab-grpc-tunnel-srl2 --skip-verify -e json_ietf set \
    --request-file config_grpc_tunnel.yaml -d

# check system/grpc-tunnel config and state
gnmic -u $username -p $password -a clab-grpc-tunnel-srl1,clab-grpc-tunnel-srl2 --skip-verify -e json_ietf get --path /system/grpc-tunnel -t config
gnmic -u $username -p $password -a clab-grpc-tunnel-srl1,clab-grpc-tunnel-srl2 --skip-verify -e json_ietf get --path /system/grpc-tunnel -t state

