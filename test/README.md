# Simple tests scripts

To show how to setup a simple VM with the images from aquarium-bait:

1. Build the macos-1015 images via aquarium-bait
2. In the directory where aquarium-fish is running create the next directory with symlinks:
```
> ls -lh fish_vmx_images/*
fish_vmx_images/macos-1015-VERSION:
total 0
lrwxr-xr-x 1 parshev staff  47 Mar 15 17:14 macos-1015 -> /Users/user/git/aquarium-bait/out/macos-1015

fish_vmx_images/macos-1015-ci-VERSION:
total 0
lrwxr-xr-x 1 parshev staff  50 Mar 15 17:14 macos-1015-ci -> /Users/user/git/aquarium-bait/out/macos-1015-ci

fish_vmx_images/macos-1015-ci-xcode122-VERSION:
total 0
lrwxr-xr-x 1 parshev staff  59 Mar 17 22:31 macos-1015-ci-xcode122 -> /Users/user/git/aquarium-bait/out/macos-1015-ci-xcode122
```
3. Now run the aquarium-fish and any `test-*` script which will guide through the resource
allocation and deallocation process
