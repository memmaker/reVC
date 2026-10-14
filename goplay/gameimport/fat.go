package gameimport

import (
	"bytes"
	"encoding/binary"
	"io"
	"strings"
)

// FAT boot sector (BIOS parameter block) and directory entry layout.
const (
	bootSectorSize    = 512
	bootSignatureAt   = 510
	bootSignature1    = 85  // 55h
	bootSignature2    = 170 // AAh
	bpbSectorSize     = 11
	bpbPerCluster     = 13
	bpbReserved       = 14
	bpbFATCount       = 16
	bpbRootEntries    = 17
	bpbTotalSectors16 = 19
	bpbFATSectors     = 22
	bpbTotalSectors32 = 32
	minSectorSize     = 128
	maxSectorSize     = 4096
	fat12MaxClusters  = 4085 // fewer clusters than this: FAT12, else FAT16
	fat12EntryMask    = 4095 // 12 bits
	firstDataCluster  = 2
	dirEntrySize      = 32
	dirNameLength     = 8
	dirExtEnd         = 11
	dirAttributes     = 11
	dirFirstCluster   = 26
	dirFileSize       = 28
	dirDeletedMarker  = 229 // E5h
	attrVolumeLabel   = 8
	attrDirectory     = 16
	maxDirectoryDepth = 16
)

// listFAT lists a FAT12/FAT16 disk image (floppy .img/.ima files and the like). Not a FAT
// image: no files, no error.
func listFAT(r io.ReaderAt, size int64) ([]file, error) {
	var boot [bootSectorSize]byte
	if _, err := r.ReadAt(boot[:], 0); err != nil || boot[bootSignatureAt] != bootSignature1 || boot[bootSignatureAt+1] != bootSignature2 {
		return nil, nil
	}
	le := binary.LittleEndian
	sectorSize := int64(le.Uint16(boot[bpbSectorSize:]))
	perCluster := int64(boot[bpbPerCluster])
	reserved := int64(le.Uint16(boot[bpbReserved:]))
	fats := int64(boot[bpbFATCount])
	rootEntries := int64(le.Uint16(boot[bpbRootEntries:]))
	totalSectors := int64(le.Uint16(boot[bpbTotalSectors16:]))
	if totalSectors == 0 {
		totalSectors = int64(le.Uint32(boot[bpbTotalSectors32:]))
	}
	fatSectors := int64(le.Uint16(boot[bpbFATSectors:]))
	if sectorSize < minSectorSize || sectorSize > maxSectorSize || perCluster == 0 || fats == 0 || fatSectors == 0 ||
		totalSectors*sectorSize > size+sectorSize {
		return nil, nil
	}
	fat := make([]byte, fatSectors*sectorSize)
	if _, err := r.ReadAt(fat, reserved*sectorSize); err != nil {
		return nil, nil
	}
	rootStart := (reserved + fats*fatSectors) * sectorSize
	rootBytes := rootEntries * dirEntrySize
	dataStart := rootStart + (rootBytes+sectorSize-1)/sectorSize*sectorSize
	clusterBytes := perCluster * sectorSize
	clusters := (totalSectors*sectorSize-dataStart)/clusterBytes + firstDataCluster
	fat12 := clusters < fat12MaxClusters

	next := func(c int64) int64 {
		if fat12 {
			v := int64(le.Uint16(fat[c*3/2:]))
			if c&1 == 1 {
				return v >> 4
			}
			return v & fat12EntryMask
		}
		return int64(le.Uint16(fat[c*2:]))
	}
	chain := func(c int64) []byte {
		var b bytes.Buffer
		for n := 0; c >= firstDataCluster && c < clusters && n < int(clusters); n++ {
			buf := make([]byte, clusterBytes)
			r.ReadAt(buf, dataStart+(c-firstDataCluster)*clusterBytes)
			b.Write(buf)
			c = next(c)
		}
		return b.Bytes()
	}

	var out []file
	var walk func(dir []byte, prefix string, depth int)
	walk = func(dir []byte, prefix string, depth int) {
		for at := 0; at+dirEntrySize <= len(dir); at += dirEntrySize {
			e := dir[at : at+dirEntrySize]
			if e[0] == 0 {
				return
			}
			attr := e[dirAttributes]
			if e[0] == dirDeletedMarker || e[0] == '.' || attr&attrVolumeLabel != 0 { // deleted, . and .., volume label/LFN
				continue
			}
			name := strings.TrimRight(string(e[:dirNameLength]), " ")
			if ext := strings.TrimRight(string(e[dirNameLength:dirExtEnd]), " "); ext != "" {
				name += "." + ext
			}
			first := int64(le.Uint16(e[dirFirstCluster:]))
			if attr&attrDirectory != 0 {
				if depth < maxDirectoryDepth {
					walk(chain(first), prefix+name+"/", depth+1)
				}
				continue
			}
			fileSize := int64(le.Uint32(e[dirFileSize:]))
			out = append(out, file{prefix + name, fileSize, func() (io.ReaderAt, error) {
				b := chain(first)
				if int64(len(b)) > fileSize {
					b = b[:fileSize]
				}
				return bytes.NewReader(b), nil
			}})
		}
	}
	root := make([]byte, rootBytes)
	if _, err := r.ReadAt(root, rootStart); err != nil {
		return nil, nil
	}
	walk(root, "", 0)
	return out, nil
}
