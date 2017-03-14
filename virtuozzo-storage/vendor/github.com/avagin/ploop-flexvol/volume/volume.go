package volume

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/dustin/go-humanize"
	"github.com/kolyshkin/goploop-cli"
)

func Create(options map[string]string) error {
	var (
		volumePath, volumeId, size string
	)

	for k, v := range options {
		switch k {
		case "volumePath":
			volumePath = v
		case "volumeId":
			volumeId = v
		case "size":
			size = v
		case "vzsReplicas":
		case "vzsTier":
		case "kubernetes.io/readwrite":
		case "kubernetes.io/fsType":
		default:
			return fmt.Errorf("Unknown option: %s (%s)", k, v)
		}
	}

	if volumePath == "" {
		return fmt.Errorf("volumePath isn't specified")
	}

	if volumeId == "" {
		return fmt.Errorf("volumeId isn't specified")
	}

	if size == "" {
		return fmt.Errorf("size isn't specified")
	}

	// get a human readable size from the map
	bytes, _ := humanize.ParseBytes(size)

	// ploop driver takes kilobytes, so convert it
	volume_size := bytes / 1024

	ploop_path := options["volumePath"] + "/" + options["volumeId"]

	// make the base directory where the volume will go
	err := os.MkdirAll(ploop_path, 0700)
	if err != nil {
		return err
	}

	for k, v := range options {
		var err error

		switch k {
		case "vzsReplicas":
			cmd := "vstorage"
			args := []string{"set-attr", "-R", ploop_path, fmt.Sprintf("replicas=%s", v)}
			err = exec.Command(cmd, args...).Run()
		case "vzsTier":
			cmd := "vstorage"
			args := []string{"set-attr", "-R", ploop_path, fmt.Sprintf("tier=%s", v)}
			err = exec.Command(cmd, args...).Run()
		}

		if err != nil {
			os.RemoveAll(ploop_path)
			return err
		}
	}

	// Create the ploop volume
	cp := ploop.CreateParam{Size: volume_size, File: ploop_path + "/" + options["volumeId"]}
	if err := ploop.Create(&cp); err != nil {
		return err
	}

	return nil
}
