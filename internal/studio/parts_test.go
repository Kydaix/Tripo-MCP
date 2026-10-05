package studio

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
)

func TestPartsFromGLB(t *testing.T) {
	data := []byte(`{"nodes":[{"name":"owl","mesh":0},{"name":"base","mesh":1}]}`)
	b := make([]byte, 20)
	copy(b, "glTF")
	binary.LittleEndian.PutUint32(b[4:], 2)
	binary.LittleEndian.PutUint32(b[8:], uint32(20+len(data)))
	binary.LittleEndian.PutUint32(b[12:], uint32(len(data)))
	copy(b[16:], "JSON")
	b = append(b, data...)
	p := filepath.Join(t.TempDir(), "fixture.glb")
	if err := os.WriteFile(p, b, 0600); err != nil {
		t.Fatal(err)
	}
	names, err := Parts(p)
	if err != nil || len(names) != 2 || names[0] != "owl" || names[1] != "base" {
		t.Fatalf("%v %v", names, err)
	}
	binary.LittleEndian.PutUint32(b[12:], 0xffffffff)
	_ = os.WriteFile(p, b, 0600)
	if _, err = Parts(p); err == nil {
		t.Fatal("oversized GLB metadata accepted")
	}
}
func TestPartsFromBinaryFBX(t *testing.T) {
	for _, wide := range []bool{false, true} {
		t.Run(map[bool]string{false: "7400", true: "7500"}[wide], func(t *testing.T) {
			b := make([]byte, 27)
			copy(b, "Kaydara FBX Binary  \x00\x1a\x00")
			version := uint32(7400)
			width := 13
			if wide {
				version = 7500
				width = 25
			}
			binary.LittleEndian.PutUint32(b[23:], version)
			props := &bytes.Buffer{}
			props.WriteByte('L')
			_ = binary.Write(props, binary.LittleEndian, int64(1))
			for _, s := range []string{"owl\x00\x01Model", "Mesh"} {
				props.WriteByte('S')
				_ = binary.Write(props, binary.LittleEndian, uint32(len(s)))
				props.WriteString(s)
			}
			objectStart := len(b)
			b = append(b, make([]byte, width)...)
			b = append(b, []byte("Objects")...)
			modelStart := len(b)
			b = append(b, make([]byte, width)...)
			b = append(b, []byte("Model")...)
			b = append(b, props.Bytes()...)
			modelEnd := len(b)
			b = append(b, make([]byte, width)...)
			objectEnd := len(b)
			b = append(b, make([]byte, width)...)
			writeNode := func(pos, end, propLen, nameLen int, count uint64) {
				if wide {
					binary.LittleEndian.PutUint64(b[pos:], uint64(end))
					binary.LittleEndian.PutUint64(b[pos+8:], count)
					binary.LittleEndian.PutUint64(b[pos+16:], uint64(propLen))
					b[pos+24] = byte(nameLen)
				} else {
					binary.LittleEndian.PutUint32(b[pos:], uint32(end))
					binary.LittleEndian.PutUint32(b[pos+4:], uint32(count))
					binary.LittleEndian.PutUint32(b[pos+8:], uint32(propLen))
					b[pos+12] = byte(nameLen)
				}
			}
			writeNode(objectStart, objectEnd, 0, 7, 0)
			writeNode(modelStart, modelEnd, props.Len(), 5, 3)
			p := filepath.Join(t.TempDir(), "fixture.fbx")
			if err := os.WriteFile(p, b, 0600); err != nil {
				t.Fatal(err)
			}
			names, err := Parts(p)
			if err != nil || len(names) != 1 || names[0] != "owl" {
				t.Fatalf("%v %v", names, err)
			}
			_ = os.WriteFile(p, b[:len(b)/2], 0600)
			if _, err = Parts(p); err == nil {
				t.Fatal("truncated FBX accepted")
			}
		})
	}
}
