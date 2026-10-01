#!/bin/zsh
set -e
server_path="$HOME/.pagehub/bin/pagehub"
sudo /usr/libexec/ApplicationFirewall/socketfilterfw --add "$server_path"
sudo /usr/libexec/ApplicationFirewall/socketfilterfw --unblockapp "$server_path"
print 'Pagehub 已允许入站连接，macOS 防火墙仍然开启。'
