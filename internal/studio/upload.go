package studio

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/Kydaix/Tripo-MCP/internal/fault"
	"github.com/aws/aws-sdk-go-v2/aws"
	v4 "github.com/aws/aws-sdk-go-v2/aws/signer/v4"
)

const MaxImageBytes = 20 << 20

type Image struct {
	Bucket string `json:"bucket"`
	Key    string `json:"key"`
	Audit  string `json:"image_audit_result,omitempty"`
	Source string `json:"image_source,omitempty"`
}

func ValidateImage(path string) (string, error) {
	if !filepath.IsAbs(path) {
		return "", fault.New("INVALID_ARGUMENT", "Le chemin de l'image doit être absolu.")
	}
	f, err := os.Open(path)
	if err != nil {
		return "", fault.New("INVALID_ARGUMENT", "Image locale introuvable ou inaccessible.")
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() == 0 || info.Size() > MaxImageBytes {
		return "", fault.New("INVALID_ARGUMENT", "Image vide, non régulière ou supérieure à 20 Mo.")
	}
	format := strings.TrimPrefix(strings.ToLower(filepath.Ext(path)), ".")
	if format == "jpeg" {
		format = "jpg"
	}
	if format != "png" && format != "jpg" && format != "webp" {
		return "", fault.New("INVALID_ARGUMENT", "Formats d'image : PNG, JPEG et WebP.")
	}
	return format, nil
}

var bucketRE = regexp.MustCompile(`^[a-z0-9][a-z0-9.-]{1,61}[a-z0-9]$`)

func (c *Client) Upload(ctx context.Context, path string) (*Image, error) {
	format, err := ValidateImage(path)
	if err != nil {
		return nil, err
	}
	var token struct {
		AK     string `json:"sts_ak"`
		SK     string `json:"sts_sk"`
		Token  string `json:"session_token"`
		Bucket string `json:"resource_bucket"`
		Key    string `json:"resource_uri"`
	}
	if err = c.request(ctx, "POST", "/v2/studio/storage/temporary_token", map[string]any{"client": "aws", "format": format}, &token, false); err != nil {
		return nil, err
	}
	if !bucketRE.MatchString(token.Bucket) || strings.Contains(token.Bucket, "..") || token.Key == "" || token.AK == "" || token.SK == "" || token.Token == "" {
		return nil, fault.New("PROTOCOL_CHANGED", "Autorisation de téléversement Studio incomplète.")
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	h := sha256.New()
	size, err := io.Copy(h, io.LimitReader(f, MaxImageBytes+1))
	if err != nil || size > MaxImageBytes {
		return nil, fault.New("INVALID_ARGUMENT", "Image illisible ou trop volumineuse.")
	}
	if _, err = f.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	u := url.URL{Scheme: "https", Host: token.Bucket + ".s3-accelerate.amazonaws.com", Path: "/" + token.Key}
	req, err := http.NewRequestWithContext(ctx, "PUT", u.String(), f)
	if err != nil {
		return nil, err
	}
	req.ContentLength = size
	req.Header.Set("Content-Type", mime.TypeByExtension(filepath.Ext(path)))
	checksum := hex.EncodeToString(h.Sum(nil))
	req.Header.Set("X-Amz-Content-Sha256", checksum)
	creds := aws.Credentials{AccessKeyID: token.AK, SecretAccessKey: token.SK, SessionToken: token.Token}
	if err = v4.NewSigner().SignHTTP(ctx, creds, req, checksum, "s3", "us-west-2", time.Now()); err != nil {
		return nil, fault.New("UPLOAD_FAILED", "Signature du téléversement impossible.")
	}
	client := &http.Client{Timeout: 5 * time.Minute, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fault.New("UPLOAD_FAILED", "Échec du téléversement de l'image.")
	}
	resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fault.New("UPLOAD_FAILED", "Le stockage a refusé l'image.")
	}
	image := &Image{Bucket: token.Bucket, Key: token.Key, Source: "upload"}
	var audit struct {
		Result string `json:"result"`
	}
	if err = c.request(ctx, "POST", "/v2/studio/audit/image", map[string]any{"image": map[string]string{"bucket": image.Bucket, "key": image.Key}}, &audit, false); err != nil {
		return nil, err
	}
	if audit.Result != "normal" {
		return nil, fault.New("IMAGE_REJECTED", "Studio n'a pas validé cette image ; vérifier celle-ci dans Studio.")
	}
	image.Audit = audit.Result
	return image, nil
}
