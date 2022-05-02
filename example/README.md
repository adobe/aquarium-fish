# Simple example scripts

This example scripts shows how to setup & allocate a simple environment with the images from
Aquarium Bait:

1. Build the docker images via aquarium-bait which will be placed into `aquarium-bait/out/docker`
directory
2. Locate the `fish_docker_images` in the workdir of the running aquarium-fish
3. Run the `fill_images.sh` like that:
   ```
   $ ./fill_images.sh <./path/to/fish_docker_images> </path/to/aquarium-bait/out/docker>
   ```
4. Now you can check & run the `create_label-*` and `run_application-*` scripts with admin token as
first argument which will guide through the resource allocation and deallocation process
