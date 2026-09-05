package downloader

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHLSEncryptedInitializationAndMedia(t *testing.T) {
	key := bytes.Repeat([]byte{1}, 16)
	block, _ := aes.NewCipher(key)
	encrypt := func(text string) []byte {
		body := []byte(text)
		padding := 16 - len(body)%16
		body = append(body, bytes.Repeat([]byte{byte(padding)}, padding)...)
		cipher.NewCBCEncrypter(block, defaultAESIVForSeq(1)).CryptBlocks(body, body)
		return body
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/index":
			fmt.Fprint(w, "#EXTM3U\n#EXT-X-KEY:METHOD=AES-128,URI=\"key\",IV=0x1\n#EXT-X-MAP:URI=\"init\"\n#EXTINF:1,\nmedia\n#EXT-X-ENDLIST\n")
		case "/key":
			w.Write(key)
		case "/init":
			w.Write(encrypt("init"))
		case "/media":
			w.Write(encrypt("media"))
		}
	}))
	defer srv.Close()
	var out bytes.Buffer
	if err := NewHLSDownloader(srv.Client(), srv.URL+"/index").Download(context.Background(), &out); err != nil {
		t.Fatal(err)
	}
	if out.String() != "initmedia" {
		t.Fatal(out.String())
	}
}
func TestHLSUnsupportedEncryptionAndEmptySegmentFail(t *testing.T) {
	if _, err := parseKey(`METHOD=SAMPLE-AES,URI="key"`, "https://example.test/index"); err == nil {
		t.Fatal("accepted unsupported encryption")
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/index" {
			fmt.Fprint(w, "#EXTM3U\n#EXTINF:1,\nempty\n#EXT-X-ENDLIST\n")
		}
	}))
	defer srv.Close()
	if err := NewHLSDownloader(srv.Client(), srv.URL+"/index").Download(context.Background(), &bytes.Buffer{}); err == nil {
		t.Fatal("accepted empty segment")
	}
}
