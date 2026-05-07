sudo systemctl stop webServer
sudo systemctl stop v5-result
sudo systemctl disable webServer
sudo systemctl disable v5-result
sudo rm /usr/lib/systemd/system/webServer.service
sudo rm /usr/lib/systemd/system/v5-result.service
sudo rm -rf /usr/local/myserver
sudo systemctl daemon-reload
echo "已删除系统服务配置文件并重新加载";
echo "------------------------------------------------";
echo "服务已停止并删除！";