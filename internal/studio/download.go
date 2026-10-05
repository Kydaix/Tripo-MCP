package studio

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Kydaix/Tripo-MCP/internal/fault"
	"github.com/Kydaix/Tripo-MCP/internal/local"
)

type Artifact struct {
	Path   string `json:"path"`
	Bytes  int64  `json:"bytes"`
	SHA256 string `json:"sha256"`
	Format string `json:"format"`
}

func assetURL(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.User != nil || (u.Port() != "" && u.Port() != "443") {
		return nil, fault.New("INVALID_ASSET", "URL de fichier non autorisée.")
	}
	h := strings.ToLower(u.Hostname())
	allowed := false
	for _, suffix := range []string{".tripo3d.ai", ".data.tripo3d.com", ".holymolly.ai", ".amazonaws.com"} {
		if strings.HasSuffix(h, suffix) {
			allowed = true
		}
	}
	if !allowed {
		return nil, fault.New("INVALID_ASSET", "Hôte de téléchargement inconnu ; vérifier le protocole Studio.")
	}
	return u, nil
}

// Download never sends session headers to storage. Files are committed only after validation.
func Download(ctx context.Context, raw, destination, format string) (Artifact, error) {
	var result Artifact
	if !oneOf(format, "glb", "fbx", "zip", "stl", "3mf", "usdz") {
		return result, fault.New("INVALID_ARGUMENT", "Format de téléchargement non pris en charge.")
	}
	u, err := assetURL(raw)
	if err != nil {
		return result, err
	}
	if !strings.EqualFold(filepath.Ext(destination), "."+format) {
		return result, invalid("L'extension de sortie doit correspondre au format " + format + ".")
	}
	destination, err = filepath.Abs(destination)
	if err != nil {
		return result, err
	}
	if _, err := os.Lstat(destination); err == nil {
		return result, fault.New("FILE_EXISTS", "Le fichier de destination existe déjà ; choisir un autre chemin.")
	}
	if err = os.MkdirAll(filepath.Dir(destination), 0700); err != nil {
		return result, err
	}
	tmp, err := os.CreateTemp(filepath.Dir(destination), ".tripo-download-*")
	if err != nil {
		return result, err
	}
	defer os.Remove(tmp.Name())
	defer tmp.Close()
	transport := http.DefaultTransport.(*http.Transport).Clone()
	dialer := &net.Dialer{Timeout: 20 * time.Second}
	transport.Proxy = nil
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, err
		}
		ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
		if err != nil {
			return nil, err
		}
		for _, ip := range ips {
			if ip.IP.IsPrivate() || ip.IP.IsLoopback() || ip.IP.IsLinkLocalUnicast() || ip.IP.IsUnspecified() {
				return nil, fault.New("INVALID_ASSET", "Adresse de stockage non publique.")
			}
		}
		for _, ip := range ips {
			conn, err := dialer.DialContext(ctx, network, net.JoinHostPort(ip.IP.String(), port))
			if err == nil {
				return conn, nil
			}
		}
		return nil, fault.New("DOWNLOAD_FAILED", "Stockage inaccessible.")
	}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 15 * time.Minute, CheckRedirect: func(r *http.Request, via []*http.Request) error {
		if len(via) > 3 {
			return http.ErrUseLastResponse
		}
		_, err := assetURL(r.URL.String())
		return err
	}}
	req, _ := http.NewRequestWithContext(ctx, "GET", u.String(), nil)
	resp, err := client.Do(req)
	if err != nil {
		return result, fault.New("DOWNLOAD_FAILED", "Téléchargement impossible ; relancer download pour obtenir un lien récent.")
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return result, fault.New("DOWNLOAD_FAILED", "Le fichier distant n'est pas disponible ; relancer download.")
	}
	const maxBytes = int64(2 << 30)
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(tmp, h), io.LimitReader(resp.Body, maxBytes+1))
	if err != nil || n > maxBytes || n == 0 {
		return result, fault.New("DOWNLOAD_FAILED", "Téléchargement incomplet, vide ou supérieur à 2 Go.")
	}
	if resp.ContentLength >= 0 && n != resp.ContentLength {
		return result, fault.New("DOWNLOAD_FAILED", "Taille du fichier reçu incorrecte.")
	}
	if _, err = tmp.Seek(0, io.SeekStart); err != nil {
		return result, err
	}
	if err = ValidateModel(tmp, n, format); err != nil {
		return result, err
	}
	if err = tmp.Sync(); err != nil {
		return result, err
	}
	if err = tmp.Close(); err != nil {
		return result, err
	}
	if err = local.CommitNew(tmp.Name(), destination); err != nil {
		return result, fault.New("DOWNLOAD_FAILED", "Impossible de finaliser le fichier ; vérifier la destination.")
	}
	return Artifact{Path: destination, Bytes: n, SHA256: hex.EncodeToString(h.Sum(nil)), Format: format}, nil
}

func ValidateModel(r io.Reader, size int64, format string) error {
	var header [84]byte
	n, _ := io.ReadFull(r, header[:])
	if format == "glb" {
		if n < 20 || string(header[:4]) != "glTF" || binary.LittleEndian.Uint32(header[4:8]) != 2 || int64(binary.LittleEndian.Uint32(header[8:12])) != size {
			return fault.New("INVALID_ASSET", "Le résultat n'est pas un fichier GLB 2 complet.")
		}
	} else if format == "fbx" && (n < 23 || (!strings.HasPrefix(string(header[:]), "Kaydara FBX Binary") && !strings.HasPrefix(string(header[:]), "; FBX"))) {
		return fault.New("INVALID_ASSET", "Le résultat n'est pas un fichier FBX reconnu.")
	} else if format == "zip" || format == "3mf" || format == "usdz" {
		at, ok := r.(io.ReaderAt)
		if !ok {
			return fault.New("INVALID_ASSET", "Archive non vérifiable.")
		}
		archive, err := zip.NewReader(at, size)
		if err != nil || len(archive.File) == 0 || len(archive.File) > 10000 {
			return fault.New("INVALID_ASSET", "Archive de modèle invalide.")
		}
		found := false
		for _, f := range archive.File {
			name := strings.ToLower(f.Name)
			switch format {
			case "3mf":
				found = found || strings.HasSuffix(name, ".model")
			case "usdz":
				found = found || oneOf(filepath.Ext(name), ".usd", ".usdc", ".usda")
			case "zip":
				found = found || oneOf(filepath.Ext(name), ".glb", ".gltf", ".fbx", ".obj", ".stl", ".3mf", ".usd", ".usdc", ".usda", ".usdz")
			}
		}
		if !found {
			return fault.New("INVALID_ASSET", "L'archive ne contient pas de modèle reconnu.")
		}
	} else if format == "stl" {
		binaryOK := n >= 84 && 84+int64(binary.LittleEndian.Uint32(header[80:84]))*50 == size
		if !binaryOK && !strings.HasPrefix(strings.TrimSpace(string(header[:n])), "solid") {
			return fault.New("INVALID_ASSET", "Fichier STL non reconnu.")
		}
	} else if !oneOf(format, "glb", "fbx") {
		return fault.New("INVALID_ASSET", "Format non vérifiable.")
	}
	return nil
}
