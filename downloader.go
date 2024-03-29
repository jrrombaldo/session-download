package main

import (
	"crypto/aes"
	"crypto/cipher"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"math/rand"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"

	"github.com/dustin/go-humanize"
	"github.com/fatih/color"
)

//magic from https://mholt.github.io/json-to-go/
type Session struct {
	SessID   int `json:"sess_id"`
	SessData struct {
		SessionName string `json:"session_name"`
		Speakers    struct {
			Num2111 struct {
				Name string      `json:"name"`
				Bio  interface{} `json:"bio"`
			} `json:"2111"`
			Num5468 struct {
				Name string      `json:"name"`
				Bio  interface{} `json:"bio"`
			} `json:"5468"`
		} `json:"speakers"`
		Desc          string `json:"desc"`
		Filename      string `json:"filename"`
		HasMp3        string `json:"has_mp3"`
		HasMp4        string `json:"has_mp4"`
		SessionNumber string `json:"session_number"`
	} `json:"sess_data"`
}

type Playlist struct {
	HTML string    `json:"html"`
	Data []Session `json:"data"`
}

type Video struct {
	URL  string `json:"url"`
	Type string `json:"type"`
	Srt  string `json:"srt"`
}

type Conference struct {
	ID        int
	Directory string
}

// sending random UserAgents on the request
var userAgents = [10]string{
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/58.0.3029.110 Safari/537.36",
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:53.0) Gecko/20100101 Firefox/53.0",
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/51.0.2704.79 Safari/537.36 Edge/14.14393",
	"Mozilla/5.0 (compatible, MSIE 11, Windows NT 6.3; Trident/7.0; rv:11.0) like Gecko",
	"Mozilla/5.0 (iPad; CPU OS 8_4_1 like Mac OS X) AppleWebKit/600.1.4 (KHTML, like Gecko) Version/8.0 Mobile/12H321 Safari/600.1.4",
	"Mozilla/5.0 (Linux; Android 6.0.1; SAMSUNG SM-N910F Build/MMB29M) AppleWebKit/537.36 (KHTML, like Gecko) SamsungBrowser/4.0 Chrome/44.0.2403.133 Mobile Safari/537.36",
	"Mozilla/5.0 (Linux; U; Android-4.0.3; en-us; Galaxy Nexus Build/IML74K) AppleWebKit/535.7 (KHTML, like Gecko) CrMo/16.0.912.75 Mobile Safari/535.7",
	"Mozilla/5.0 (Linux; Android 5.0; SAMSUNG SM-N900 Build/LRX21V) AppleWebKit/537.36 (KHTML, like Gecko) SamsungBrowser/2.1 Chrome/34.0.1847.76 Mobile Safari/537.36",
	"Mozilla/5.0 (compatible; MSIE 10.0; Windows NT 6.2; Trident/6.0; MDDCJS)",
	"Mozilla/4.0 (compatible; MSIE 8.0; Windows NT 5.1; Trident/4.0; .NET CLR 1.1.4322; .NET CLR 2.0.50727; .NET CLR 3.0.4506.2152; .NET CLR 3.5.30729)",
}

// global variable that receives the server hostname/domain
// lazy to propagate on the calls
var hostname = ""

// if any error happens terminate execution
// TODO improve error handling and propagation
func handleError(err error, msg string) {
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, color.RedString(msg+"\n"+err.Error()))
		os.Exit(1)
	}
}

// receive the querystring and returns the URL
func buildUrl(params url.Values) string {
	// assuming the hostname is being populated
	var baseUrl, err = url.Parse("https://" + hostname)
	handleError(err, "BaseUrl")
	baseUrl.Path += "player"
	if params != nil {
		baseUrl.RawQuery = params.Encode()
	}
	return baseUrl.String()
}

func executeGETRequest(params url.Values) string {
	var request, err = http.NewRequest("GET", buildUrl(params), nil)
	handleError(err, "NewRequest")

	// setting handle userAgents headers on each request
	request.Header.Set("UserAgent", userAgents[rand.Int31n(10)])

	var httpClient = &http.Client{}
	resp, err := httpClient.Do(request)
	handleError(err, "DoRequest")

	defer resp.Body.Close()
	dataInBytes, err := ioutil.ReadAll(resp.Body)

	return string(dataInBytes)
}

func getPlaylist(sessionId int, dir string) Playlist {
	fmt.Println("  -> retrieving playlist for " + strconv.Itoa(sessionId))
	var params = url.Values{}
	params.Add("action", "get_playlist")
	params.Add("conf_id", strconv.Itoa(sessionId))

	var resp = executeGETRequest(params)

	var playlist Playlist
	if err := json.NewDecoder(strings.NewReader(resp)).Decode(&playlist); err != nil {
		handleError(err, "Parsing Playlist")
	}
	fmt.Println("  -> found [",
		color.CyanString(strconv.Itoa(len(playlist.Data))),
		"] sessions on this playlist")

	// writing index
	indexContent, _ := json.MarshalIndent(playlist, "", " ")
	var err = ioutil.WriteFile(
		dir+string(os.PathSeparator)+"index.json",
		indexContent,
		0644)
	handleError(err, "WritingIndex")
	return playlist
}

func getVideo(sessionId int) Video {
	fmt.Println("  -> geting session data for ",
		color.CyanString(strconv.Itoa(sessionId)))
	var params = url.Values{}
	params.Add("action", "get_video")
	params.Add("session_id", strconv.Itoa(sessionId))

	var resp = executeGETRequest(params)

	var session Video
	if err := json.NewDecoder(strings.NewReader(resp)).Decode(&session); err != nil {
		handleError(err, "Parsing Playlist")
	}
	return session
}

type WriteCounter struct {
	Total    uint64
	FileSize uint64
}

func (wc *WriteCounter) Write(p []byte) (int, error) {
	n := len(p)
	wc.Total += uint64(n)
	wc.PrintProgress()
	return n, nil
}

func (wc WriteCounter) PrintProgress() {
	// Clear the line by using a character return to go back to the start and remove
	// the remaining characters by filling it with spaces
	fmt.Printf("\r%s", strings.Repeat(" ", 35))

	// Return again and print current status of download
	// We use the humanize package to print the bytes in a meaningful way (e.g. 10 MB)
	fmt.Printf("\rDownloading... %s / %s complete",
		humanize.Bytes(wc.Total), humanize.Bytes(wc.FileSize))
	//fmt.Printf("\rDownloading... %s complete", humanize.Bytes(wc.Total))
}

func FileExists(filename string) bool {
	info, err := os.Stat(filename)
	if os.IsNotExist(err) {
		return false
	}
	return !info.IsDir()
}

func DownloadVideo(session Session, dir string) error {
	var name string = session.SessData.SessionName
	// seems like the IDs are not constant
	//var name string = strconv.Itoa(session.SessID) + " - " + session.SessData.SessionName

	//var extension = strings.Replace(video.Type, "video/", ".", -1)
	var extension = ".mp4" // only get video if file does not exists
	if len(name) > 256 {
		name = name[:256]
	}
	name = strings.Replace(name, "/", "-", -1)
	name = name + extension // increases the file name length

	var downloadingPrefix = ".downloading"
	var filepath = dir + string(os.PathSeparator) + name

	// if file already exists, skip download
	if FileExists(filepath) {
		fmt.Println("  -> ["+filepath+"] ",
			color.YellowString("already downloaded"))
		return nil
	}

	// if file temporary files exists, remove it
	if FileExists(filepath + downloadingPrefix) {
		fmt.Println("  -> file ["+filepath+"] ",
			color.MagentaString("already exist, removing..."))
		err := os.Remove(filepath + downloadingPrefix)
		if err != nil {
			fmt.Println("  -> can not delete file " + filepath + downloadingPrefix)
			return err
		}
	}

	fmt.Println("  -> downloading " + color.GreenString(filepath))
	var video = getVideo(session.SessID)

	out, err := os.Create(filepath + downloadingPrefix)
	handleError(err, "CreatingFile")
	defer out.Close()

	resp, err := http.Get(video.URL)
	handleError(err, "DownloadRequest")
	defer resp.Body.Close()

	// getting video size
	size, _ := strconv.Atoi(resp.Header.Get("Content-Length"))

	counter := &WriteCounter{}
	counter.FileSize = uint64(size)
	_, err = io.Copy(out, io.TeeReader(resp.Body, counter))
	handleError(err, "WritingFile")

	// The progress use the same line so print a new line once it's finished downloading
	fmt.Print("\n")

	err = os.Rename(filepath+downloadingPrefix, filepath)
	handleError(err, "RenameDownloadedFile")

	writeVideoDetails(session, dir)

	return nil
}

func writeVideoDetails(session Session, dir string) {
	// writing the session abstract
	var sessionDetails = session.SessData.SessionName
	sessionDetails += "\n\n\n"
	sessionDetails += session.SessData.Desc
	sessionDetails += "\n\n"
	//sessionDetails += strings.(session.SessData.Speakers)

	//oinly write if file does not exists already
	var filename = dir + string(os.PathSeparator) + strings.Replace(session.SessData.SessionName, "/", "-", -1) + ".txt"
	if !FileExists(filename) {
		var err = ioutil.WriteFile(
			filename,
			[]byte(sessionDetails),
			0644)
		handleError(err, "WritingSessionDetail")
	}
}

func encryptHostname(hostname string, keyb64 string) string {
	var key, _ = base64.StdEncoding.DecodeString(keyb64)
	nonce := key[:12] // why bother when the key is just above :)
	plaintext1 := []byte("www.sok-media.com")
	block, _ := aes.NewCipher(key)
	aesgcm, _ := cipher.NewGCM(block)
	ciphertext1 := aesgcm.Seal(nil, nonce, plaintext1, nil)
	b64Ciphertext := base64.StdEncoding.EncodeToString([]byte(ciphertext1))
	return b64Ciphertext

}
func getHostname(ciphertextb64 string, keyb64 string) string {
	var key, _ = base64.StdEncoding.DecodeString(keyb64)
	ciphertext, _ := base64.StdEncoding.DecodeString(ciphertextb64)
	block, _ := aes.NewCipher(key)
	aesgcm, _ := cipher.NewGCM(block)
	plaintext, _ := aesgcm.Open(nil, key[:12], ciphertext, nil)
	return string(plaintext)
}

func main() {
	ciphertext := "JJrO/dE3tmur31VMr/CCN19WbyxZGLQ/WK7EPqW9vLos" // ecnrypted hostname
	if len(os.Args) != 2 {
		fmt.Println("missing aes key")
		os.Exit(1)
	} else {
		hostname = getHostname(ciphertext, os.Args[1])
	}

	//
	//
	//			>>>>>>> ADD CONFERENCES MORE HERE <<<<<<<<<
	//
	//
	var conferences = []Conference{
		Conference{70, "./BlackHat-2019"},
		Conference{71, "./DefCon-2019"},
		Conference{72, "./DefConVilla-2019"},
		Conference{73, "./BesideS-2019"},
		Conference{67, "./DefCon1"},
		Conference{62, "./BesideS1"},
		Conference{61, "./BlackHat1"},
		Conference{59, "./BlackHat2"},
		Conference{55, "./BesideS2"},
		Conference{54, "./DefCon2"},
		Conference{53, "./BlackHat3"},
		Conference{51, "./DefCon3"},
		Conference{50, "./BlackHat4"},
		Conference{46, "./BesideS3"},
		Conference{45, "./BlackHat5"},
		Conference{41, "./DefCon4"},
		Conference{40, "./BlackHat6"},
		Conference{39, "./BesideS4"},
	}

	for _, conf := range conferences {

		_ = os.Mkdir(conf.Directory, 0755)

		var playlist Playlist = getPlaylist(conf.ID, conf.Directory) //besides

		for _, session := range playlist.Data {
			var err = DownloadVideo(session, conf.Directory)
			handleError(err, "DownloadingFile")
			fmt.Println(strings.Repeat("=", 74))
		}
	}

	// var playlist Playlist = getPlaylist(70, dir) //blackhat
	// var playlist Playlist = getPlaylist(71, dir) //defcon
	//var playlist Playlist = getPlaylist(72, dir) //defcon villa

}
