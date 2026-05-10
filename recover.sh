sudo systemctl stop webServer
sudo systemctl stop v5-result
sudo systemctl disable webServer
sudo systemctl disable v5-result
sudo rm /usr/lib/systemd/system/webServer.service
sudo rm /usr/lib/systemd/system/v5-result.service
sudo cp /usr/local/webServer/webServer.service /usr/lib/systemd/system/webServer.service
sudo cp /usr/local/gzbao_server/v5-result/v5-result.service /usr/lib/systemd/system/v5-result.service
sudo systemctl daemon-reload
sudo systemctl enable webServer
sudo systemctl start webServer
sudo systemctl enable v5-result
sudo systemctl start v5-result
echo "已恢复系统服务配置文件并重新加载";
echo "------------------------------------------------";
# sudo cp server_conf.json /usr/local/myserver/webServer/server_conf.json
# sudo cp v5_conf.json /usr/local/myserver/v5-result/v5_conf.json
# sudo systemctl restart webServer
# sudo systemctl restart v5-result