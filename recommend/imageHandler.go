// SPDX-License-Identifier: Apache-2.0
// Copyright 2022 Authors of KubeArmor

package recommend

import (
	"archive/tar"
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/client"
	"github.com/docker/docker/pkg/jsonmessage"
	"github.com/moby/term"
	log "github.com/sirupsen/logrus"
)

var cli *client.Client // docker client
var tempDir string     // temporary directory used by karmor to save image etc

// ImageInfo contains image information
type ImageInfo struct {
	Name     string
	RepoTags []string
	Arch     string
	OS       string
	FileList []string
	DirList  []string
}

func getAuthStr() string {
	u := os.Getenv("DOCKER_USERNAME")
	p := os.Getenv("DOCKER_PASSWORD")
	if u == "" || p == "" {
		return ""
	}

	encodedJSON, err := json.Marshal(types.AuthConfig{
		Username: u,
		Password: p,
	})
	if err != nil {
		log.WithError(err).Fatal("failed to marshal credentials")
	}

	return base64.URLEncoding.EncodeToString(encodedJSON)
}

func init() {
	var err error

	rand.Seed(time.Now().UnixNano()) // random seed init for random string generator

	cli, err = client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		log.WithError(err).Fatal("could not create new docker client")
	}
}

func pullImage(imageName string) error {
	out, err := cli.ImagePull(context.Background(), imageName, types.ImagePullOptions{RegistryAuth: getAuthStr()})
	if err != nil {
		log.WithError(err).Fatal("could not pull image")
	}
	defer out.Close()

	termFd, isTerm := term.GetFdInfo(os.Stderr)
	err = jsonmessage.DisplayJSONMessagesStream(out, os.Stderr, termFd, isTerm, nil)
	if err != nil {
		log.WithError(err).Error("could not display json")
	}

	return nil
}

// The randomizer used in this function is not used for any cryptographic
// operation and hence safe to use.
func randString(n int) string {
	var letterRunes = []rune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ")
	b := make([]rune, n)
	for i := range b {
		b[i] = letterRunes[rand.Intn(len(letterRunes))] // #nosec
	}
	return string(b)
}

func closeCheckErr(f *os.File, fname string) {
	err := f.Close()
	if err != nil {
		log.WithFields(log.Fields{
			"file": fname,
		}).Error("close file failed")
	}
}

// Sanitize archive file pathing from "G305: Zip Slip vulnerability"
func sanitizeArchivePath(d, t string) (v string, err error) {
	v = filepath.Join(d, t)
	if strings.HasPrefix(v, filepath.Clean(d)) {
		return v, nil
	}

	return "", fmt.Errorf("%s: %s", "content filepath is tainted", t)
}

func extractTar(tarname string) ([]string, []string) {
	var fl []string
	var dl []string

	f, err := os.Open(filepath.Clean(tarname))
	if err != nil {
		log.WithError(err).WithFields(log.Fields{
			"tar": tarname,
		}).Fatal("os create failed")
	}
	defer closeCheckErr(f, tarname)

	tr := tar.NewReader(bufio.NewReader(f))
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break // End of archive
		}
		if err != nil {
			log.WithError(err).Fatal("tar next failed")
		}

		tgt, err := sanitizeArchivePath(tempDir, hdr.Name)
		if err != nil {
			log.WithError(err).WithFields(log.Fields{
				"file": hdr.Name,
			}).Error("ignoring file since it could not be sanitized")
			continue
		}

		switch hdr.Typeflag {
		case tar.TypeDir:
			if _, err := os.Stat(tgt); err != nil {
				if err := os.MkdirAll(tgt, 0750); err != nil {
					log.WithError(err).WithFields(log.Fields{
						"target": tgt,
					}).Fatal("tar mkdirall")
				}
			}
			dl = append(dl, tgt)
		case tar.TypeReg:
			f, err := os.OpenFile(filepath.Clean(tgt), os.O_CREATE|os.O_RDWR, os.FileMode(hdr.Mode))
			if err != nil {
				log.WithError(err).WithFields(log.Fields{
					"target": tgt,
				}).Fatal("tar open file")
			}

			// copy over contents
			if _, err := io.CopyN(f, tr, 2e+8 /*200MB*/); err != io.EOF {
				log.WithError(err).WithFields(log.Fields{
					"target": tgt,
				}).Fatal("tar io.Copy()")
			}
			closeCheckErr(f, tgt)
			if strings.HasSuffix(tgt, "layer.tar") { // deflate container image layer
				ifl, idl := extractTar(tgt)
				fl = append(fl, ifl...)
				dl = append(dl, idl...)
			} else {
				fl = append(fl, tgt)
			}
		}
	}
	return fl, dl
}

func saveImageToTar(imageName string) string {
	imgdata, err := cli.ImageSave(context.Background(), []string{imageName})
	if err != nil {
		log.WithError(err).Fatal("could not save image")
	}
	defer imgdata.Close()

	tarname := filepath.Join(tempDir, randString(8)+".tar")

	f, err := os.Create(filepath.Clean(tarname))
	if err != nil {
		log.WithError(err).Fatal("os create failed")
	}

	if _, err := io.CopyN(bufio.NewWriter(f), imgdata, 5e+9 /*5GB*/); err != io.EOF {
		log.WithError(err).WithFields(log.Fields{
			"tar": tarname,
		}).Fatal("io.CopyN() failed")
	}
	closeCheckErr(f, tarname)
	log.WithFields(log.Fields{
		"tar": tarname,
	}).Info("dumped image to tar")
	return tarname
}

func checkForSpec(spec string, fl []string) []string {
	var matches []string
	re := regexp.MustCompile(spec)
	for _, name := range fl {
		if re.Match([]byte(name)) {
			matches = append(matches, name)
		}
	}
	return matches
}

func getFileBytes(fname string) ([]byte, error) {
	f, err := os.Open(filepath.Clean(fname))
	if err != nil {
		log.WithFields(log.Fields{
			"file": fname,
		}).Fatal("open file failed")
	}
	defer closeCheckErr(f, fname)
	return io.ReadAll(f)
}

func readManifest(img *ImageInfo, manifest string) {
	// read manifest file
	barr, err := getFileBytes(manifest)
	if err != nil {
		log.WithFields(log.Fields{
			"manifest": manifest,
		}).Fatal("manifest read failed")
	}
	var manres []map[string]interface{}
	err = json.Unmarshal(barr, &manres)
	if err != nil {
		log.WithError(err).Fatal("manifest json unmarshal failed")
	}
	if len(manres) != 1 {
		log.WithFields(log.Fields{
			"len":     len(manres),
			"results": manres,
		}).Fatal("expecting one config in manifest!")
	}
	// 	man := manres.(map[string]interface{})

	// read config file
	config := filepath.Join(tempDir, manres[0]["Config"].(string))
	barr, err = getFileBytes(config)
	if err != nil {
		log.WithFields(log.Fields{
			"config": config,
		}).Fatal("config read failed")
	}
	var cfgres map[string]interface{}
	err = json.Unmarshal(barr, &cfgres)
	if err != nil {
		log.WithError(err).Fatal("config json unmarshal failed")
	}
	img.Arch = cfgres["architecture"].(string)
	img.OS = cfgres["os"].(string)
	for _, tag := range manres[0]["RepoTags"].([]interface{}) {
		img.RepoTags = append(img.RepoTags, tag.(string))
		// img.RepoTags = manres[0]["RepoTags"].([]interface{}).([]string)
	}
}

func getImageInfo(img *ImageInfo) {
	matches := checkForSpec(filepath.Join(tempDir, "manifest.json"), img.FileList)
	if len(matches) != 1 {
		log.WithFields(log.Fields{
			"len":     len(matches),
			"matches": matches,
		}).Fatal("expecting one manifest.json!")
	}
	readManifest(img, matches[0])
}

func getImageDetails(imageName string) error {
	var img ImageInfo
	img.Name = imageName
	// step 1: save the image to a tar file
	tarname := saveImageToTar(imageName)

	// step 2: retrieve information from tar
	img.FileList, img.DirList = extractTar(tarname)

	// step 3: getImageInfo
	getImageInfo(&img)

	getPolicyFromImageInfo(&img)
	// Check if os == linux
	// Check if certificates folder exists
	// Check if package management tools exists
	return nil
}

func imageHandler(imageName string) error {
	log.WithFields(log.Fields{
		"image": imageName,
	}).Info("pulling image")
	err := pullImage(imageName)
	if err != nil {
		return err
	}

	err = getImageDetails(imageName)
	if err != nil {
		return err
	}

	return nil
}
