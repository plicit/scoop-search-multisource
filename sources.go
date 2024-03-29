package main

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"log"
	"net/http"
	net_url "net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/valyala/fastjson"
)

// routes to the appropriate bucket source loader based on `kind` or url/file format
func loadBucketsFrom(kind string, path string) (buckets BucketMap, err error) {
	switch kind {
	case "buckets":
		switch {
		case strings.HasSuffix(path, ".json"):
			buckets, err = loadBucketsFromNameSourceJson(path)
		default: // assume it is a local path
			buckets = loadBucketsFromDir(path)
		}
	case "html":
		buckets, err = loadBucketsFromHtml(path)
	default:
		var appList AppList
		if strings.Contains(path, "://") {
			switch {
			case strings.HasSuffix(path, ".html") || strings.HasSuffix(path, ".htm"):
				buckets, err = loadBucketsFromHtml(path)
			case strings.HasSuffix(path, ".zip") || strings.Contains(path, "/zipball/"):
				appList = loadAppListFromZipUrl(path)
			default:
				appList = loadAppListFromGitRepoUrl(path)
			}
		} else { // it's a local file
			// normalize path separator
			path = strings.ReplaceAll(path, "/", "\\")
			switch {
			case strings.HasSuffix(path, ".html") || strings.HasSuffix(path, ".htm"):
				readCloser, err := os.Open(path)
				check(err)
				buckets, err = loadBucketsFromHtmlReader(readCloser)
				check(err)
			case strings.HasSuffix(path, ".zip"):
				appList = loadAppListFromZip(path)
			default:
				appList = loadAppListFromDir(path)
			}
		}
		if buckets == nil {
			buckets = BucketMap{path: appList}
		}
	}
	return
}

// searches for term in given json manifest
// this does NOT set app.name, since that is not in the json
// https://github.com/ScoopInstaller/scoop/wiki/App-Manifests
func loadAppFromManifest(manifestPath string, json []byte) (app *AppInfo) {
	finalMsg := ""
	defer func() { // catch if fastjson panics
		// recover from panic if one occured. Set err to nil otherwise.
		if recover() != nil {
			app = nil
			fmt.Printf(colorize("error", "*** Skipped BROKEN manifest: %s\n"), manifestPath)
		} else if finalMsg != "" {
			fmt.Printf(colorize("error", "*** Including BROKEN manifest (%s): %s\n"), finalMsg, manifestPath)
		}
	}()

	app = &AppInfo{}

	var parser fastjson.Parser
	result, _ := parser.ParseBytes(json)

	version := string(result.GetStringBytes("version"))
	description := string(result.GetStringBytes("description"))
	homepage := string(result.GetStringBytes("homepage"))

	var bins []string
	bin := result.Get("bin") // can be: nil, string, [](string | []string)
	if bin != nil {
		errMsg := `bad "bin"`
		switch bin.Type() {
		case fastjson.TypeString:
			// "bin": "myprog.exe",
			bins = append(bins, string(bin.GetStringBytes()))
		case fastjson.TypeArray:
			// "bin": [ "myprog.exe", [ "program.exe", "alias", "--arg1" ] ]
			for _, stringOrArray := range bin.GetArray() {
				switch stringOrArray.Type() {
				case fastjson.TypeString: // "myprog.exe"
					bins = append(bins, string(stringOrArray.GetStringBytes()))
				case fastjson.TypeArray: // [ "program.exe", "alias", "--arg1", "--arg2" ]
					// check only the first two strings, the rest are command flags
					stringArray := stringOrArray.GetArray()
					// caused panic when there was only one string:
					//bins = append(bins, string(stringArray[0].GetStringBytes()), string(stringArray[1].GetStringBytes()))
					count := 0
					for _, item := range stringArray {
						bins = append(bins, string(item.GetStringBytes()))
						count += 1
						// max of 2: exe and alias
						if count >= 2 {
							break
						}
					}
				default:
					finalMsg = errMsg
					//log.Fatalln(badManifestErrMsg)
				}
			}
		default:
			finalMsg = errMsg
			//log.Fatalln(badManifestErrMsg)
		}
	}

	app.Version = version
	app.Description = description
	app.Homepage = homepage
	app.Bins = bins
	//app.loaded = true

	return app
}

// currently only searches given path for ./bucket/*.json or else ./*.json
func loadAppListFromDir(path string) (apps AppList) {
	subBucketPath := filepath.Join(path, "bucket")
	if f, err := os.Stat(subBucketPath); !os.IsNotExist(err) && f.IsDir() {
		path = subBucketPath
	}

	fileInfos, err := os.ReadDir(path)
	check(err)

	for _, fileInfo := range fileInfos {
		name := fileInfo.Name()

		// it's not a manifest, skip
		if !strings.HasSuffix(name, ".json") {
			continue
		}

		filePath := filepath.Join(path, name)
		body, err := os.ReadFile(filePath)
		check(err)

		// parse relevant data from manifest
		app := loadAppFromManifest(filePath, body)
		if app != nil {
			app.Name = name[:len(name)-5]
			apps = append(apps, app)
		}
	}

	return apps
}

func loadBucketsFromDir(bucketsPath string) (buckets BucketMap) {
	bucketDirEntries, err := os.ReadDir(bucketsPath)
	checkWith(err, "Buckets folder does not exist")

	var mutex sync.Mutex
	var wg sync.WaitGroup

	buckets = BucketMap{}
	for _, bucketDirEntry := range bucketDirEntries {
		wg.Add(1)
		go func(file os.DirEntry) {
			bucketName := file.Name()
			bucketPath := filepath.Join(bucketsPath, bucketName)
			appList := loadAppListFromDir(bucketPath)

			mutex.Lock()
			buckets[bucketPath] = appList
			mutex.Unlock()

			wg.Done()
		}(bucketDirEntry)
	}
	wg.Wait()
	return
}

func loadInstalledApps(appsPath string, apps *NameSourceMap) (err error) {
	appFileInfos, err := os.ReadDir(appsPath)
	if err != nil {
		return err
	}

	//apps = NameSourceMap{}
	for _, appFileInfo := range appFileInfos {
		name := appFileInfo.Name()
		path := filepath.Join(appsPath, name)
		(*apps)[name] = path
	}
	return nil
}

//=========================================================
// ZIP: Load a Bucket's App List from a local .zip
//=========================================================

func loadAppListFromZip(path string) (appList AppList) {
	zipReader, err := zip.OpenReader(path)
	check(err)
	defer zipReader.Close()

	// appxxx.json can either be in the root or in a /bucket/ subdirectory at any depth
	re_appManifestPath := regexp.MustCompile(`(^|(?:^|/|\\)bucket(?:/|\\))([^/\\]*)\.json$`)

	for _, file := range zipReader.Reader.File {
		innerPath := file.Name // path within zip file
		//fmt.Printf("innerPath = %#v\n", innerPath)

		// only search *.json in bucket/ or root
		if re_appManifestPath.MatchString(innerPath) {
			// uncompress file body
			readCloser, err := file.Open()
			check(err)
			body, err := io.ReadAll(readCloser)
			readCloser.Close()
			check(err)

			filePath := fmt.Sprintf("%s:%s", path, innerPath)
			app := loadAppFromManifest(filePath, body)
			if app != nil {
				_, filename := filepath.Split(innerPath)
				app.Name = filename[:len(filename)-5] // remove ".json"
				appList = append(appList, app)
			}
		}
	}
	return
}

// downloads a bucket as a zip and searches its manifests
func loadAppListFromZipUrl(url string) (appList AppList) {
	cachePath := filepath.Join(scoopCache("buckets"), net_url.QueryEscape(url)+".zip")
	cacheGetUrl(cachePath, url)
	appList = loadAppListFromZip(cachePath)
	return appList
}

//=========================================================
// HTML: Load buckets from Html (primarily rasa's directory)
//=========================================================

func loadBucketsFromHtml(url string) (buckets BucketMap, err error) {
	var bodyReader io.ReadCloser

	filePath := url
	if strings.Contains(url, "://") {
		// add .html just in case the url doesn't include it
		filePath = filepath.Join(scoopCache("buckets"), net_url.QueryEscape(url)+".html")
		err = cacheGetUrl(filePath, url)
		if err != nil {
			// if cache doesn't exist
			f, err2 := os.Stat(filePath)
			if os.IsNotExist(err2) {
				return
			}
			// error downloading, but we have a stale cache we can use
			fmt.Printf("Failed to download, so using stale cache from %s ...", f.ModTime().Format(time.RFC1123Z))
		}
	} // else the url is already a filepath

	bodyReader, _ = os.Open(filePath)

	return loadBucketsFromHtmlReader(bodyReader)
}

func loadBucketsFromHtmlReader(body io.ReadCloser) (buckets BucketMap, err error) {
	var headings, row []string
	var rows [][]string

	doc, err := goquery.NewDocumentFromReader(body)
	if err != nil {
		return
	}

	buckets = BucketMap{}

	// For each table
	doc.Find("table").Each(func(tablei int, table *goquery.Selection) {

		headings, row, rows = nil, nil, nil
		//fmt.Println("\n---------- Found table! ----------")
		table.Find("tr").Each(func(tri int, tr *goquery.Selection) {
			tr.Find("th").Each(func(thi int, th *goquery.Selection) {
				headings = append(headings, strings.TrimSpace(th.Text()))
			})

			tr.Find("td").Each(func(tdi int, td *goquery.Selection) {
				row = append(row, strings.TrimSpace(td.Text()))
			})
			if row != nil {
				rows = append(rows, row)
				row = nil
			}
		})

		// find index of columns we are interested in
		namei, versioni, descriptioni := -1, -1, -1
		for icol, col := range headings {
			label := strings.ToLower(col)
			switch {
			case strings.Contains(label, "name"):
				namei = icol
			case strings.Contains(label, "ver"):
				versioni = icol
			case strings.Contains(label, "desc"):
				descriptioni = icol
			}
		}
		// fmt.Printf("namei=%d, versioni=%d, descriptioni=%d\n", namei, versioni, descriptioni)
		// fmt.Printf("headings = %v\n", headings)
		// for _, row := range rows {
		// 	fmt.Printf("row = %v\n", row)
		// }

		if namei < 0 { // then this is not an appInfo table (we at least need a name column that contains the repo url)
			return // continue with next table
		}

		source := ""
		missingSource := true
		// get the bucket source url
		table.PrevUntil("table").EachWithBreak(func(anchori int, node *goquery.Selection) bool {
			node.Find("a").EachWithBreak(func(anchori int, anchor *goquery.Selection) bool {
				//prev, _ := goquery.OuterHtml(anchor)
				//fmt.Printf("===== node: %s\n", prev)
				href, exists := anchor.Attr("href")
				if exists && strings.Contains(href, "github.com") {
					source = href
					missingSource = false
				}
				return missingSource // assume that the first github href pertains to this table
			})
			return missingSource
		})
		//fmt.Printf("*** source: %v", source)

		apps := AppList{}

		for _, row := range rows {
			app := AppInfo{}
			app.Name = strings.TrimSpace(row[namei])
			app.Version = strings.TrimSpace(row[versioni])
			app.Description = strings.TrimSpace(row[descriptioni])
			//fmt.Printf("app=%#v\n", app)
			apps = append(apps, &app)

		}

		buckets[source] = apps
	})

	return
}

//=========================================================
// Git: Loads a Bucket by first cloning a repo url
//=========================================================

// Load a bucket by locally cloning a Git repo
func cacheGitRepo(url string) (repoPath string) {

	repoPath = filepath.Join(scoopCache("buckets"), net_url.QueryEscape(url))

	var stdout bytes.Buffer
	var cmdline []string

	_, err := os.Stat(filepath.Join(repoPath, ".git"))
	if os.IsNotExist(err) {
		cmdline = []string{"git", "clone", url, repoPath}
		log.Println("Cloning repository: " + url)
	} else {
		cmdline = []string{"git", "pull"}
		log.Println("Updating repository cache: " + repoPath)
	}

	cmd := exec.Command(cmdline[0], cmdline[1:]...)
	cmd.Stdout = &stdout
	cmd.Stderr = &stdout // &stderr
	cmd.Dir = repoPath
	err = cmd.Run()
	fmt.Println(stdout.String())

	if err != nil {
		fmt.Printf(colorize("error", "*** %s\nTrying to continue anyway..."), err)
	}

	return
}

// Clones a Git repo and loades its apps
func loadAppListFromGitRepoUrl(url string) (appList AppList) {
	fmt.Printf("loadAppListFromGitRepoUrl: %s\n", url)
	path := cacheGitRepo(url)
	return loadAppListFromDir(path)
}

//=========================================================
// CACHE management for network requests
//=========================================================

var g_CacheDuration time.Duration = 24 * time.Hour

//func init() {
//	g_CacheDuration = 24 * time.Hour
//}

func scoopCache(category string) (cachePath string) {
	cachePath = filepath.Join(g_Config.ScoopCacheDir, category)
	checkWith(os.MkdirAll(cachePath, 0700), "Can't create cache directory: "+cachePath)
	return
}

func cacheGetUrl(cacheFilePath string, url string) (err error) {
	err = nil

	now := time.Now()
	f, err := os.Stat(cacheFilePath)
	cache_exists := !os.IsNotExist(err)

	age := time.Duration(0)
	if cache_exists {
		age = now.Sub(f.ModTime())
	}

	if !cache_exists || g_CacheDuration < age {
		fmt.Printf("Downloading: %s\n", url)

		cli := http.Client{}
		if g_Config.ScoopProxy != "" {
			// https://github.com/ScoopInstaller/Scoop/wiki/Using-Scoop-behind-a-proxy#config-examples
			proxy := "http://" + g_Config.ScoopProxy
			url, err := net_url.Parse(proxy)
			// todo proxy password containing `@` or `:`
			if err == nil {
				transport := &http.Transport{Proxy: http.ProxyURL(url)}
				cli.Transport = transport

				switch tv := http.DefaultTransport.(type) {
				case *http.Transport:
					{
						transport.DialContext = tv.DialContext
						transport.ForceAttemptHTTP2 = tv.ForceAttemptHTTP2
						transport.MaxIdleConns = tv.MaxIdleConns
						transport.IdleConnTimeout = tv.IdleConnTimeout
						transport.TLSHandshakeTimeout = tv.TLSHandshakeTimeout
						transport.ExpectContinueTimeout = tv.ExpectContinueTimeout
					}
				}
			}
		}

		response, err := cli.Get(url)
		if err != nil { // default to cache
			return err
		}
		defer response.Body.Close()

		if response.StatusCode != 200 {
			log.Fatalf("failed to fetch data: %d %s", response.StatusCode, response.Status)
		}

		cacheFile, err := os.Create(cacheFilePath)
		checkWith(err, "Couldn't create cache file")

		// save to cache file
		_, err = io.Copy(cacheFile, response.Body)
		checkWith(err, "Couldn't write to cache file")

	} else {
		fmt.Printf(colorize("source.status", "using %s old cache: %s\n"), fmtDuration(age), cacheFilePath)
	}

	return
}

//=========================================================
// buckets.json: load json and the buckets it references
//=========================================================

// loads a buckets.json file into a map[name]sourceUrl
func loadNameSourceMapFromJsonFile(path string) (res NameSourceMap) {
	raw, err := os.ReadFile(path)
	check(err)
	res = loadNameSourceMapFromJson(raw)
	return
}

func loadNameSourceMapFromJson(json []byte) (res NameSourceMap) {
	//fmt.Printf("buckets.json:\n%s\n", raw)

	var parser fastjson.Parser
	v, _ := parser.ParseBytes(json)
	o, _ := v.Object()

	res = NameSourceMap{}
	o.Visit(func(bucketName []byte, url *fastjson.Value) {
		res[string(bucketName)] = string(url.GetStringBytes())
	})

	return
}

func loadBucketsFromNameSourceJson(path string) (buckets BucketMap, err error) {
	buckets = make(BucketMap)
	var more_buckets BucketMap
	nameSourceMap := loadNameSourceMapFromJsonFile(path)
	for _, source := range nameSourceMap {
		more_buckets, err = loadBucketsFrom("bucket", source)
		// aggregate buckets
		for name, appList := range more_buckets {
			buckets[name] = appList
		}
	}
	return
}
