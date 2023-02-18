# Example of setup the fish node on Linux

This document shows how to setup aquarium fish service on Linux node. Not all the commands are
necessary and here mostly to show the general path of how to make it right.

The commands are executed as root on ubuntu linux on x64 arch. Make sure you know what you doing
for each step and adjust for your system.

1. Create non previleged user for fish node:
   ```sh
   useradd aquarium-fish
   ```

2. (optional) Install docker to use it as resource provider:
   ```sh
   apt update
   apt install docker.io
   usermod -aG docker aquarium-fish
   ```

3. Create main service folder for fish node:
   ```sh
   mkdir -p /srv/aquarium-fish/ws
   chown aquarium-fish:aquarium-fish /srv/aquarium-fish/ws
   ```

4. Copy aquarium-fish binary to the right place:
   ```sh
   cp aquarium-fish.linux_amd64 /srv/aquarium-fish/aquarium-fish
   chmod +x /srv/aquarium-fish/aquarium-fish
   ```

5. Create config file to define the node:
   ```yaml
   ---
    node_name: test-node.corp.example.com
    node_location: us-west-2a
    default_resource_lifetime: 1h30m

    # In case you configure the drivers - only the defined drivers will be activated
    drivers:
      # Configure docker here for example
      - name: docker
        cfg:
          download_user: user
          download_password: password
   ```
   ```sh
   vim /srv/aquarium-fish/config.yml
   chown root:aquarium-fish config.yml
   ```

6. Run aquarium-fish first time to get admin creds, then press Ctrl-C to stop it:
   WARNING: This command will create SSL CA which will last for 1 year. Please check openssl docs
   to generate better CA and certificates for nodes in your cluster.
   ```sh
   su - aquarium-fish sh -c 'cd /srv/aquarium-fish/ws && ../aquarium-fish -c ../config.yml'
   ```

7. Copy the unit service file to the systemd configs
   ```sh
   cp systemd/aquarium-fish.service /etc/systemd/system/
   ```

8. Enable for autostart and start the service:
   ```sh
   systemctl enable aquarium-fish.service
   systemctl start aquarium-fish.service
   ```

9. (optional) Check logs to see that the service is started ok:
   ```sh
   journalctl -fu aquarium-fish.service
   ```
