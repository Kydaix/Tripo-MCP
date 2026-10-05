package studio

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"io"
	"os"
	"strings"
)

// Parts reads mesh names without a DCC dependency. Studio requires real part
// names even for a one-mesh texture request; [] does not mean "all".
func Parts(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	var header [27]byte
	if _, err = f.ReadAt(header[:], 0); err != nil {
		return nil, err
	}
	var names []string
	if string(header[:4]) == "glTF" {
		n := int64(binary.LittleEndian.Uint32(header[12:16]))
		if n > 16<<20 || n < 2 || n+20 > info.Size() || string(header[16:20]) != "JSON" {
			return nil, invalid("Métadonnées GLB non reconnues ; fournir parts explicitement.")
		}
		b := make([]byte, n)
		if _, err = f.ReadAt(b, 20); err != nil {
			return nil, err
		}
		var model struct {
			Nodes []struct {
				Name string `json:"name"`
				Mesh *int   `json:"mesh"`
			} `json:"nodes"`
		}
		if json.Unmarshal(bytes.TrimRight(b, "\x00 "), &model) != nil {
			return nil, invalid("Métadonnées GLB invalides.")
		}
		for _, node := range model.Nodes {
			if node.Mesh != nil {
				if node.Name == "" {
					return nil, invalid("Un mesh GLB n'a pas de nom ; fournir parts explicitement.")
				}
				names = append(names, node.Name)
			}
		}
	} else if strings.HasPrefix(string(header[:]), "Kaydara FBX Binary") {
		wide := binary.LittleEndian.Uint32(header[23:27]) >= 7500
		var walk func(int64, int64, bool) error
		walk = func(start, limit int64, objects bool) error {
			for pos, count := start, 0; pos < limit; count++ {
				if count > 1000000 {
					return invalid("Trop de nœuds FBX.")
				}
				width := 13
				if wide {
					width = 25
				}
				h := make([]byte, width)
				if _, err := f.ReadAt(h, pos); err != nil {
					return err
				}
				var end, propLen uint64
				var nameLen byte
				if wide {
					end = binary.LittleEndian.Uint64(h)
					propLen = binary.LittleEndian.Uint64(h[16:])
					nameLen = h[24]
				} else {
					end = uint64(binary.LittleEndian.Uint32(h))
					propLen = uint64(binary.LittleEndian.Uint32(h[8:]))
					nameLen = h[12]
				}
				if end == 0 {
					return nil
				}
				props := pos + int64(width) + int64(nameLen)
				if end > uint64(limit) || end <= uint64(props) || propLen > end-uint64(props) {
					return invalid("Structure FBX invalide.")
				}
				name := make([]byte, nameLen)
				if _, err := f.ReadAt(name, pos+int64(width)); err != nil {
					return err
				}
				if !objects && string(name) == "Objects" {
					if err := walk(props+int64(propLen), int64(end), true); err != nil {
						return err
					}
				}
				if objects && string(name) == "Model" {
					if propLen > 1<<20 {
						return invalid("Métadonnées FBX trop volumineuses.")
					}
					b := make([]byte, propLen)
					if _, err := f.ReadAt(b, props); err != nil {
						return err
					}
					label, kind, err := fbxModelProperties(b)
					if err != nil {
						return err
					}
					if kind == "Mesh" {
						names = append(names, label)
					}
				}
				pos = int64(end)
			}
			return nil
		}
		if err = walk(27, info.Size(), false); err != nil {
			return nil, err
		}
	} else {
		return nil, invalid("Fournir parts explicitement pour ce format de modèle.")
	}
	unique := []string{}
	seen := map[string]bool{}
	for _, name := range names {
		if name != "" && !seen[name] {
			seen[name] = true
			unique = append(unique, name)
		}
	}
	if len(unique) == 0 {
		return nil, invalid("Aucun nom de mesh trouvé ; fournir parts explicitement.")
	}
	return unique, nil
}
func fbxModelProperties(b []byte) (string, string, error) {
	r := bytes.NewReader(b)
	stringsFound := []string{}
	for r.Len() > 0 {
		kind, err := r.ReadByte()
		if err != nil {
			return "", "", err
		}
		switch kind {
		case 'L', 'D':
			if _, err = r.Seek(8, io.SeekCurrent); err != nil {
				return "", "", err
			}
		case 'I', 'F':
			if _, err = r.Seek(4, io.SeekCurrent); err != nil {
				return "", "", err
			}
		case 'Y':
			if _, err = r.Seek(2, io.SeekCurrent); err != nil {
				return "", "", err
			}
		case 'C':
			if _, err = r.ReadByte(); err != nil {
				return "", "", err
			}
		case 'S':
			var n uint32
			if binary.Read(r, binary.LittleEndian, &n) != nil || uint64(n) > uint64(r.Len()) {
				return "", "", invalid("Propriété FBX invalide.")
			}
			s := make([]byte, n)
			_, _ = io.ReadFull(r, s)
			text := strings.SplitN(string(s), "\x00", 2)[0]
			stringsFound = append(stringsFound, text)
		default:
			return "", "", invalid("Propriété de nœud FBX non reconnue ; fournir parts explicitement.")
		}
	}
	if len(stringsFound) != 2 {
		return "", "", invalid("Nom de mesh FBX non reconnu.")
	}
	return strings.TrimPrefix(stringsFound[0], "Model::"), stringsFound[1], nil
}
