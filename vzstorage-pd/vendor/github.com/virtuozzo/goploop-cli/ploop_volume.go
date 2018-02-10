package ploop

import (
	"fmt"
	"os"
	"path"
	"strconv"
)

var (
	ploopVolumeBinary = "/usr/sbin/ploop-volume"
)

type PloopVolume struct {
	Path string
}

type PloopVolumeSnapshot struct {
	Path string
}

func checkDD(src string) error {
	if _, err := os.Stat(path.Join(src, "DiskDescriptor.xml")); os.IsNotExist(err) {
		return &Err{c: -1, s: fmt.Sprintf("Bad ploop-volume path %s!", src)}
	}
	return nil
}

func PloopVolumeOpen(src string) (*PloopVolume, error) {
	if err := checkDD(src); err != nil {
		return nil, err
	}
	return &PloopVolume{src}, nil
}

func PloopVolumeSnapshotOpen(src string) (*PloopVolumeSnapshot, error) {
	if err := checkDD(src); err != nil {
		return nil, err
	}
	return &PloopVolumeSnapshot{src}, nil
}

func PloopVolumeCreate(src string, size uint64, image string) (*PloopVolume, error) {
	args := []string{"create", "-s", strconv.FormatUint(size, 10) + "K"}
	if image != "" {
		args = append(args, "--image", image)
	}
	args = append(args, src)
	if err := ploopVolume(args...); err != nil {
		return nil, err
	}
	return &PloopVolume{src}, nil
}

func (pv *PloopVolume) Snapshot(dst string) (*PloopVolumeSnapshot, error) {
	if dst == "" {
		return nil, &Err{c: -1, s: fmt.Sprintf("Bad destination path!")}
	}
	err := ploopVolume("snapshot", pv.Path, dst)
	if err != nil {
		return nil, err
	}
	return &PloopVolumeSnapshot{dst}, nil
}

func (pvs *PloopVolumeSnapshot) Switch(pv PloopVolume) error {
	if err := checkDD(pv.Path); err != nil {
		return err
	}
	if err := checkDD(pvs.Path); err != nil {
		return err
	}
	return ploopVolume("switch", pvs.Path, pv.Path)
}

func (pvs *PloopVolumeSnapshot) Clone(dst string) (*PloopVolume, error) {
	if err := checkDD(pvs.Path); err != nil {
		return nil, err
	}
	err := ploopVolume("clone", pvs.Path, dst)
	if err != nil {
		return nil, err
	}
	return &PloopVolume{dst}, nil
}

func (pv *PloopVolume) Delete() error {
	return ploopVolume("delete", pv.Path)
}

func (pvs *PloopVolumeSnapshot) Delete() error {
	return ploopVolume("delete", pvs.Path)
}
