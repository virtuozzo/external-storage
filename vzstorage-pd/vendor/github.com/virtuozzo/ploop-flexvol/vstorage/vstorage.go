package vstorage

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"syscall"
)

const FUSE_SUPER_MAGIC = 0x65735546

type Vstorage struct {
	Name string
}

type Mntent struct {
	Device string
	Path   string
	Type   string
}

func IsVstorage(path string) (bool, error) {
	var buf syscall.Statfs_t
	if err := syscall.Statfs(path, &buf); err != nil {
		return false, fmt.Errorf("Unable to get filesystem statistics for %s: %v", path, err)
	}
	return buf.Type == FUSE_SUPER_MAGIC, nil
}

func readMounts(path string) ([]Mntent, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	out := []Mntent{}
	r := bufio.NewReader(f)
	for {
		line, err := r.ReadString('\n')
		if err == io.EOF {
			break
		}
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}

		out = append(out, Mntent{
			Device: fields[0],
			Path:   fields[1],
			Type:   fields[2],
		})
	}
	return out, nil
}

func (v *Vstorage) Mountpoint() (string, error) {
	// find out cluster mount point
	mounts, err := readMounts("/proc/mounts")
	if err != nil {
		return "", fmt.Errorf("Unable to parse /proc/mounts: %v", err)
	}
	mount := ""
	for _, m := range mounts {
		if m.Type == "fuse.vstorage" && m.Device == "vstorage://"+v.Name {
			mount = m.Path
			break
		}
	}
	return mount, nil
}

func (v *Vstorage) Auth(password string) error {
	auth := exec.Command("vstorage", "-c", v.Name, "auth-node", "-P")
	var b bytes.Buffer
	b.Write([]byte(password))
	auth.Stdin = &b
	_, err := auth.Output()
	if err != nil {
		return fmt.Errorf("Unable to authenticate the node in %s: %v", v.Name, err)
	}
	return nil
}

func (v *Vstorage) Mount(where string) error {
	mount := exec.Command("vstorage-mount", "-c", v.Name, where)
	_, err := mount.Output()
	if err != nil {
		return fmt.Errorf("Unable to mount %s in %s: %v", v.Name, where, err)
	}
	return nil
}
