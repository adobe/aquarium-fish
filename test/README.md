# Simple tests scripts

To show how to setup a simple VM with the images from aquarium-bait:

1. Build the required images via aquarium-bait which will be placed into `aquarium-bait/out` dir.

2. Locate the `fish_vmx_images` in the workdir of aquarium-fish.

3. Run the `fill_images.sh` like that:
   ```
   $ ./fill_images.sh <./path/to/fish_vmx_images> </path/to/aquarium-bait/out>
   ```

4. Now you can run the aquarium-fish node and required `run-*` script with admin token as first
argument which will guide through the resource allocation and deallocation process
