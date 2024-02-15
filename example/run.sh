#!/bin/bash


version="0.1.8"
username=admin
password=NokiaSrl1!
#nodes="clab-grpc-tunnel-srl1,clab-grpc-tunnel-srl2"
nodes="clab-grpc-tunnel-srl1"

# download the deb file
rm -rf pkg/
wget -qL https://github.com/karimra/srl-grpc-tunnel/releases/download/v${version}/srl-grpc-tunnel_${version}_Linux_x86_64.deb -P pkg/

#deploy the lab
sudo clab deploy -t grpc_tunnel.clab.yaml --reconfigure 

# enable gnmi unix-socket
gnmic -u $username -p $password -a $nodes --skip-verify -e json_ietf set \
    --request-file confg_gnmi_unix_sock.yaml

# ACLs
gnmic -u $username -p $password -a $nodes --skip-verify -e json_ietf set \
    --request-file acls.yaml

# install the pkg located in /tmp/pkg
sudo clab exec --topo grpc_tunnel.clab.yaml --label clab-node-kind=srl --label containerlab=grpc-tunnel --cmd "sudo dpkg -i /tmp/pkg/srl-grpc-tunnel_${version}_Linux_x86_64.deb"

sleep 15
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
gnmic -u $username -p $password -a $nodes --skip-verify -e json_ietf set \
    --request-file config_grpc_tunnel.yaml -d

# check system/grpc-tunnel config and state
gnmic -u $username -p $password -a $nodes --skip-verify -e json_ietf get --path /system/grpc-tunnel -t config
gnmic -u $username -p $password -a $nodes --skip-verify -e json_ietf get --path /system/grpc-tunnel -t state
