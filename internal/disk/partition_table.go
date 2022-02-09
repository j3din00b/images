package disk

import (
	"errors"
	"fmt"
	"math/rand"

	"github.com/google/uuid"
)

type PartitionTable struct {
	Size       uint64 // Size of the disk (in bytes).
	UUID       string // Unique identifier of the partition table (GPT only).
	Type       string // Partition table type, e.g. dos, gpt.
	Partitions []Partition

	SectorSize   uint64 // Sector size in bytes
	ExtraPadding uint64 // Extra space at the end of the partition table (sectors)
}

func (pt *PartitionTable) IsContainer() bool {
	return true
}

func (pt *PartitionTable) Clone() Entity {
	if pt == nil {
		return nil
	}

	clone := &PartitionTable{
		Size:         pt.Size,
		UUID:         pt.UUID,
		Type:         pt.Type,
		Partitions:   make([]Partition, len(pt.Partitions)),
		SectorSize:   pt.SectorSize,
		ExtraPadding: pt.ExtraPadding,
	}

	for idx, partition := range pt.Partitions {
		ent := partition.Clone()
		var part *Partition

		if ent != nil {
			pEnt, cloneOk := ent.(*Partition)
			if !cloneOk {
				panic("PartitionTable.Clone() returned an Entity that cannot be converted to *PartitionTable; this is a programming error")
			}
			part = pEnt
		}
		clone.Partitions[idx] = *part
	}
	return clone
}

// AlignUp will align the given bytes to next aligned grain if not already
// aligned
func (pt *PartitionTable) AlignUp(size uint64) uint64 {
	grain := DefaultGrainBytes
	if size%grain == 0 {
		// already aligned: return unchanged
		return size
	}
	return ((size + grain) / grain) * grain
}

// Convert the given bytes to the number of sectors.
func (pt *PartitionTable) BytesToSectors(size uint64) uint64 {
	sectorSize := pt.SectorSize
	if sectorSize == 0 {
		sectorSize = DefaultSectorSize
	}
	return size / sectorSize
}

// Convert the given number of sectors to bytes.
func (pt *PartitionTable) SectorsToBytes(size uint64) uint64 {
	sectorSize := pt.SectorSize
	if sectorSize == 0 {
		sectorSize = DefaultSectorSize
	}
	return size * sectorSize
}

func (pt *PartitionTable) FindPartitionForMountpoint(mountpoint string) *Partition {
	for idx, p := range pt.Partitions {
		if p.Payload == nil {
			continue
		}

		if p.Payload.Mountpoint == mountpoint {
			return &pt.Partitions[idx]
		}
	}

	return nil
}

// Returns the root partition (the partition whose filesystem has / as
// a mountpoint) of the partition table. Nil is returned if there's no such
// partition.
func (pt *PartitionTable) RootPartition() *Partition {
	return pt.FindPartitionForMountpoint("/")
}

// Returns the /boot partition (the partition whose filesystem has /boot as
// a mountpoint) of the partition table. Nil is returned if there's no such
// partition.
func (pt *PartitionTable) BootPartition() *Partition {
	return pt.FindPartitionForMountpoint("/boot")
}

// Returns the index of the boot partition: the partition whose filesystem has
// /boot as a mountpoint.  If there is no explicit boot partition, the root
// partition is returned.
// If neither boot nor root partitions are found, returns -1.
func (pt *PartitionTable) BootPartitionIndex() int {
	// find partition with '/boot' mountpoint and fallback to '/'
	rootIdx := -1
	for idx, part := range pt.Partitions {
		if part.Payload == nil {
			continue
		}
		if part.Payload.Mountpoint == "/boot" {
			return idx
		} else if part.Payload.Mountpoint == "/" {
			rootIdx = idx
		}
	}
	return rootIdx
}

// StopIter is used as a return value from iterator function to indicate
// the iteration should not continue. Not an actual error and thus not
// returned by iterator function.
var StopIter = errors.New("stop the iteration")

// ForEachFileSystemFunc is a type of function called by ForEachFilesystem
// to iterate over every filesystem in the partition table.
//
// If the function returns an error, the iteration stops.
type ForEachFileSystemFunc func(fs *Filesystem) error

// Iterates over all filesystems in the partition table and calls the
// callback on each one. The iteration continues as long as the callback
// does not return an error.
func (pt *PartitionTable) ForEachFilesystem(cb ForEachFileSystemFunc) error {
	for _, part := range pt.Partitions {
		if part.Payload == nil {
			continue
		}

		if err := cb(part.Payload); err != nil {
			if err == StopIter {
				return nil
			}
			return err
		}
	}

	return nil
}

// Returns the Filesystem instance for a given mountpoint, if it exists.
func (pt *PartitionTable) FindFilesystemForMountpoint(mountpoint string) *Filesystem {
	var res *Filesystem
	_ = pt.ForEachFilesystem(func(fs *Filesystem) error {
		if fs.Mountpoint == mountpoint {
			res = fs
			return StopIter
		}

		return nil
	})

	return res
}

// Returns if the partition table contains a filesystem with the given
// mount point.
func (pt *PartitionTable) ContainsMountpoint(mountpoint string) bool {
	return pt.FindFilesystemForMountpoint(mountpoint) != nil
}

// Returns the Filesystem instance that corresponds to the root
// filesystem, i.e. the filesystem whose mountpoint is '/'.
func (pt *PartitionTable) RootFilesystem() *Filesystem {
	return pt.FindFilesystemForMountpoint("/")
}

// Returns the Filesystem instance that corresponds to the boot
// filesystem, i.e. the filesystem whose mountpoint is '/boot',
// if /boot is on a separate partition, otherwise nil
func (pt *PartitionTable) BootFilesystem() *Filesystem {
	return pt.FindFilesystemForMountpoint("/boot")
}

// Create a new filesystem within the partition table at the given mountpoint
// with the given minimum size in bytes.
func (pt *PartitionTable) CreateFilesystem(mountpoint string, size uint64) error {
	filesystem := Filesystem{
		Type:         "xfs",
		Mountpoint:   mountpoint,
		FSTabOptions: "defaults",
		FSTabFreq:    0,
		FSTabPassNo:  0,
	}

	partition := Partition{
		Size:    size,
		Payload: &filesystem,
	}

	n := len(pt.Partitions)
	var maxNo int

	if pt.Type == "gpt" {
		partition.Type = FilesystemDataGUID
		maxNo = 128
	} else {
		maxNo = 4
	}

	if n == maxNo {
		return fmt.Errorf("maximum number of partitions reached (%d)", maxNo)
	}

	pt.Partitions = append(pt.Partitions, partition)

	return nil
}

// Generate all needed UUIDs for all the partiton and filesystems
//
// Will not overwrite existing UUIDs and only generate UUIDs for
// partitions if the layout is GPT.
func (pt *PartitionTable) GenerateUUIDs(rng *rand.Rand) {
	_ = pt.ForEachFilesystem(func(fs *Filesystem) error {
		if fs.UUID == "" {
			fs.UUID = uuid.Must(newRandomUUIDFromReader(rng)).String()
		}
		return nil
	})

	// if this is a MBR partition table, there is no need to generate
	// uuids for the partitions themselves
	if pt.Type != "gpt" {
		return
	}

	for idx, part := range pt.Partitions {
		if part.UUID == "" {
			pt.Partitions[idx].UUID = uuid.Must(newRandomUUIDFromReader(rng)).String()
		}
	}
}

func (pt *PartitionTable) GetItemCount() uint {
	return uint(len(pt.Partitions))
}

func (pt *PartitionTable) GetChild(n uint) Entity {
	return &pt.Partitions[n]
}

func (pt *PartitionTable) GetSize() uint64 {
	return pt.Size
}

func (pt *PartitionTable) EnsureSize(s uint64) bool {
	if s > pt.Size {
		pt.Size = s
		return true
	}
	return false
}

type EntityCallback func(e Entity, path []Entity) error

func forEachEntity(e Entity, path []Entity, cb EntityCallback) error {

	childPath := append(path, e)
	err := cb(e, childPath)

	if err != nil {
		return err
	}

	c, ok := e.(Container)
	if !ok {
		return nil
	}

	for idx := uint(0); idx < c.GetItemCount(); idx++ {
		child := c.GetChild(idx)
		err = forEachEntity(child, childPath, cb)
		if err != nil {
			return err
		}
	}

	return nil
}

// ForEachEntity runs the provided callback function on each entity in
// the PartitionTable.
func (pt *PartitionTable) ForEachEntity(cb EntityCallback) error {
	return forEachEntity(pt, []Entity{}, cb)
}

// Dynamically calculate and update the start point for each of the existing
// partitions. Adjusts the overall size of image to either the supplied
// value in `size` or to the sum of all partitions if that is lager.
// Will grow the root partition if there is any empty space.
// Returns the updated start point.
func (pt *PartitionTable) updatePartitionStartPointOffsets(size uint64) uint64 {
	// always reserve one extra sector for the GPT header
	header := pt.SectorsToBytes(1)
	footer := uint64(0)

	if pt.Type == "gpt" {

		// calculate the space we need for
		parts := len(pt.Partitions)

		// reserve a minimum of 128 partition entires
		if parts < 128 {
			parts = 128
		}

		header += uint64(parts * 128)

		footer = header
	}

	start := pt.AlignUp(header)
	size = pt.AlignUp(size)

	var rootIdx = -1
	for idx := range pt.Partitions {
		partition := &pt.Partitions[idx]
		if len(entityPath(partition, "/")) != 0 {
			rootIdx = idx
			continue
		}
		partition.Start = start
		partition.Size = pt.AlignUp(partition.Size)
		start += partition.Size
	}

	if rootIdx < 0 {
		panic("no root filesystem found; this is a programming error")
	}

	root := &pt.Partitions[rootIdx]
	root.Start = start

	// add the extra padding specified in the partition table
	footer += pt.ExtraPadding

	// If the sum of all partitions is bigger then the specified size,
	// we use that instead. Grow the partition table size if needed.
	end := pt.AlignUp(root.Start + footer + root.Size)
	if end > size {
		size = end
	}

	if size > pt.Size {
		pt.Size = size
	}

	// If there is space left in the partition table, grow root
	root.Size = pt.Size - root.Start

	// Finally we shrink the last partition, i.e. the root partition,
	// to leave space for the footer, e.g. the secondary GPT header.
	root.Size -= footer

	return start
}

// entityPath stats at ent and searches for an Entity with a Mountpoint equal
// to the target. Returns a slice of all the Entities leading to the Mountable
// in reverse order. If no Entity has the target as a Mountpoint, returns nil.
// If a slice is returned, the last element is always the starting Entity ent
// and the first element is always a Mountable with a Mountpoint equal to the
// target.
func entityPath(ent Entity, target string) []Entity {
	switch e := ent.(type) {
	case Mountable:
		if target == e.GetMountpoint() {
			return []Entity{ent}
		}
	case Container:
		for idx := uint(0); idx < e.GetItemCount(); idx++ {
			child := e.GetChild(idx)
			path := entityPath(child, target)
			if path != nil {
				path = append(path, e)
				return path
			}
		}
	}
	return nil
}

type MountableCallback func(mnt Mountable, path []Entity) error

func forEachMountable(c Container, path []Entity, cb MountableCallback) error {
	for idx := uint(0); idx < c.GetItemCount(); idx++ {
		child := c.GetChild(idx)
		childPath := append(path, child)
		var err error
		switch ent := child.(type) {
		case Mountable:
			err = cb(ent, childPath)
		case Container:
			err = forEachMountable(ent, childPath, cb)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// ForEachMountable runs the provided callback function on each Mountable in
// the PartitionTable.
func (pt *PartitionTable) ForEachMountable(cb MountableCallback) error {
	return forEachMountable(pt, []Entity{pt}, cb)
}
