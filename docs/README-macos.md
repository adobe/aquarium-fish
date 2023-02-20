# Example of setup the fish node on MacOSX

This document shows how to setup aquarium fish service on MacOS node. Not all the commands are
necessary and here mostly to show the general path of how to make it right.

The commands are executed as root on MacOS 11 on x64 arch. Make sure you know what you doing for
each step and adjust for your system.

1. Create non previleged user for fish node:
   ```sh
   dscl . -create /Groups/_aquarium-fish
   dscl . -create /Groups/_aquarium-fish PrimaryGroupID 300

   dscl . -create /Users/_aquarium-fish UniqueID 300
   dscl . -create /Users/_aquarium-fish PrimaryGroupID 300
   dscl . -create /Users/_aquarium-fish UserShell /bin/sh
   dscl . -create /Users/_aquarium-fish NFSHomeDirectory /Users/aquarium-fish
   mkdir /Users/aquarium-fish
   chown _aquarium-fish:_aquarium-fish /Users/aquarium-fish
   ```

2. (optional) Install VMware Fusion to use it as resource provider:
   ```sh
   mkdir /tmp/vmwarefusion
   hdiutil attach VMware-Fusion-13.0.1-21139760_universal.dmg -nobrowse -mountpoint /tmp/vmwarefusion
   cp -a '/tmp/vmwarefusion/VMware Fusion.app/Contents/Library/Deploy VMware Fusion.mpkg' /tmp/
   cp -a '/tmp/vmwarefusion/VMware Fusion.app' '/tmp/Deploy VMware Fusion.mpkg/Contents/00Fusion_Deployment_Items/'
   hdiutil detach /tmp/vmwarefusion
   sed -i.bak -e 's/^# key =.*/key = <YOUR_LICENSE_KEY>/' '/tmp/Deploy VMware Fusion.mpkg/Contents/00Fusion_Deployment_Items/Deploy.ini'
   installer -pkg '/tmp/Deploy VMWare Fusion.mpkg' -target /
   reboot
   ```

3. Create main service folder for fish node:
   ```sh
   mkdir -p /opt/aquarium-fish/ws
   chown _aquarium-fish:_aquarium-fish /opt/aquarium-fish/ws
   ```

4. Copy aquarium-fish binary to the right place:
   ```sh
   tar xf aquarium-fish-v*.darwin_amd64.tar.xz
   mv aquarium-fish /opt/aquarium-fish/aquarium-fish
   chmod +x /opt/aquarium-fish/aquarium-fish
   ```

5. Create config file to define the node:
   ```yaml
   ---
   node_name: test-node-mac.corp.example.com
   node_location: us-west-2a
   default_resource_lifetime: 1h30m

   # In case you configure the drivers - only the defined drivers will be activated
   drivers:
     # Configure vmx here for example
     - name: vmx
       cfg:
         download_user: user
         download_password: password
   ```
   ```sh
   vim /opt/aquarium-fish/config.yml
   chown root:_aquarium-fish /opt/aquarium-fish/config.yml
   chmod 640 /opt/aquarium-fish/config.yml
   ```

6. Run aquarium-fish first time to get admin creds, then press Ctrl-C to stop it:
   WARNING: This command will create SSL CA which will last for 1 year. Please check openssl docs
   to generate better CA and certificates for nodes in your cluster.
   ```sh
   su _aquarium-fish -c "sh -c 'cd /opt/aquarium-fish/ws && ../aquarium-fish -c ../config.yml'"
   ```

7. Copy the unit service file to the systemd configs and touch the log file
   ```sh
   cp launchd/aquarium.fish.plist /Library/LaunchDaemons/
   touch /opt/aquarium-fish/fish.log
   chown root:_aquarium-fish /opt/aquarium-fish/fish.log
   chmod 660 /opt/aquarium-fish/fish.log
   ```

8. Enable for autostart and start the service:
   ```sh
   launchctl load /Library/LaunchDaemons/aquarium.fish.plist
   systemctl start aquarium-fish.service
   ```

9. (optional) Check logs to see that the service is started ok:
   ```sh
   journalctl -fu aquarium-fish.service
   ```
