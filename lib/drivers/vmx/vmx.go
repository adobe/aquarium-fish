package vmx

// VMWare VMX (Fusion/Workstation) driver to manage VMs & images

import (
	"bytes"
	"fmt"
	"log"
	//"os"
	"os/exec"
	"path"
	"regexp"
	"strings"

	"git.corp.adobe.com/CI/aquarium-fish/lib/drivers"
)

// Implements drivers.ResourceDriver interface
type Driver struct {
	cfg Config
}

func init() {
	drivers.DriversList = append(drivers.DriversList, &Driver{})
}

func (d *Driver) Name() string {
	return "vmx"
}

func (d *Driver) Prepare(config []byte) error {
	if err := d.cfg.ApplyConfig(config); err != nil {
		return err
	}
	if err := d.cfg.ValidateConfig(); err != nil {
		return err
	}
	return nil
}

func (d *Driver) Allocate(labels []string) error {
	vmx_path := path.Join(d.cfg.WorkspacePath, "test_vm/test_vm.vmx") // TODO

	// Clone
	args := []string{"-T", "fusion", "clone",
		path.Join(d.cfg.ImagesPath, "macos-1015-xcode/macos-1015-xcode.vmx"), // TODO: use labels
		vmx_path, // TODO: use resource id
		"linked", "-snapshot", "original",
	}
	cmd := exec.Command(d.cfg.VmrunPath, args...)

	/*var out bytes.Buffer
		var err bytes.Buffer
	    cmd.Stdout = &out
	    cmd.Stderr = &err

		if err := cmd.Run(); err != nil {
			log.Println("VMX: Failed to clone VM image:", d.cfg.VmrunPath, args)
			log.Printf("VMX:   stdout: %q\n", stdout.String())
			log.Printf("VMX:   stderr: %q\n", stderr.String())
			return err
		}*/

	if _, _, err := runAndLog(cmd); err != nil {
		return err
	}

	// Run
	cmd = exec.Command(d.cfg.VmrunPath, "-T", "fusion", "start", vmx_path, "nogui")
	if _, _, err := runAndLog(cmd); err != nil {
		return err
	}

	return nil
}

func (d *Driver) Status(labels []string) string {
	cmd := exec.Command(d.cfg.VmrunPath, "-T", "fusion", "list")
	stdout, _, err := runAndLog(cmd)
	if err != nil {
		return ""
	}

	vmx_path := path.Join(d.cfg.WorkspacePath, "test_vm/test_vm.vmx") // TODO

	for _, line := range strings.Split(stdout, "\n") {
		if line == vmx_path {
			return line
		}
	}
	return ""
}

func (d *Driver) Deallocate(labels []string) error {
	vmx_path := path.Join(d.cfg.WorkspacePath, "test_vm/test_vm.vmx") // TODO

	cmd := exec.Command(d.cfg.VmrunPath, "-T", "fusion", "stop", vmx_path, "hard")
	if _, _, err := runAndLog(cmd); err != nil {
		// Check if the VM is running. If it's not, it was already stopped
		if running := d.Status(nil /*vmx_path*/); running == "" {
			return nil
		}

		return err
	}

	// Delete VM
	cmd = exec.Command(d.cfg.VmrunPath, "-T", "fusion", "deleteVM", vmx_path)
	if _, _, err := runAndLog(cmd); err != nil {
		return err
	}
	/*if err := os.RemoveAll(path.Join(d.cfg.WorkspacePath, "test_vm")); err != nil {
		return err
	}*/

	return nil
}

// Directly from packer: github.com/hashicorp/packer
func runAndLog(cmd *exec.Cmd) (string, string, error) {
	var stdout, stderr bytes.Buffer

	log.Printf("VMX: Executing: %s %s", cmd.Path, strings.Join(cmd.Args[1:], " "))
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()

	stdoutString := strings.TrimSpace(stdout.String())
	stderrString := strings.TrimSpace(stderr.String())

	if _, ok := err.(*exec.ExitError); ok {
		message := stderrString
		if message == "" {
			message = stdoutString
		}

		err = fmt.Errorf("VMware error: %s", message)

		// If "unknown error" is in there, add some additional notes
		re := regexp.MustCompile(`(?i)unknown error`)
		if re.MatchString(message) {
			err = fmt.Errorf(
				"%s\n\n%s", err,
				"Packer detected a VMware 'Unknown Error'. Unfortunately VMware\n"+
					"often has extremely vague error messages such as this and Packer\n"+
					"itself can't do much about that. Please check the vmware.log files\n"+
					"created by VMware when a VM is started (in the directory of the\n"+
					"vmx file), which often contains more detailed error information.\n\n"+
					"You may need to set the command line flag --on-error=abort to\n\n"+
					"prevent Packer from cleaning up the vmx file directory.")
		}
	}

	log.Printf("VMX: stdout: %s", stdoutString)
	log.Printf("VMX: stderr: %s", stderrString)

	// Replace these for Windows, we only want to deal with Unix
	// style line endings.
	returnStdout := strings.Replace(stdout.String(), "\r\n", "\n", -1)
	returnStderr := strings.Replace(stderr.String(), "\r\n", "\n", -1)

	return returnStdout, returnStderr, err
}
