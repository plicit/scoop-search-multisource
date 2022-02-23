package main

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"io/ioutil"
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
func loadAppFromManifest(json []byte) (app *AppInfo) {
	app = &AppInfo{}

	var parser fastjson.Parser
	result, _ := parser.ParseBytes(json)

	version := string(result.GetStringBytes("version"))
	description := string(result.GetStringBytes("description"))
	homepage := string(result.GetStringBytes("homepage"))

	var bins []string
	bin := result.Get("bin") // can be: nil, string, [](string | []string)
	if bin != nil {
		const badManifestErrMsg = `Cannot parse "bin" attribute in a manifest. This should not happen. Please open an issue about it with steps to reproduce`

		switch bin.Type() {
		case fastjson.TypeString:
			bins = append(bins, string(bin.GetStringBytes()))
		case fastjson.TypeArray:
			for _, stringOrArray := range bin.GetArray() {
				switch stringOrArray.Type() {
				case fastjson.TypeString:
					bins = append(bins, string(stringOrArray.GetStringBytes()))
				case fastjson.TypeArray:
					// check only first two, the rest are command flags
					stringArray := stringOrArray.GetArray()
					bins = append(bins, string(stringArray[0].GetStringBytes()), string(stringArray[1].GetStringBytes()))
				default:
					log.Fatalln(badManifestErrMsg)
				}
			}
		default:
			log.Fatalln(badManifestErrMsg)
		}
	}

	app.version = version
	app.description = description
	app.homepage = homepage
	app.bins = bins
	//app.loaded = true

	return app
}

// currently only searches given path for ./bucket/*.json or else ./*.json
func loadAppListFromDir(path string) (apps AppList) {
	subBucketPath := filepath.Join(path, "bucket")
	if f, err := os.Stat(subBucketPath); !os.IsNotExist(err) && f.IsDir() {
		path = subBucketPath
	}

	fileInfos, err := ioutil.ReadDir(path)
	check(err)

	for _, fileInfo := range fileInfos {
		name := fileInfo.Name()

		// it's not a manifest, skip
		if !strings.HasSuffix(name, ".json") {
			continue
		}

		body, err := ioutil.ReadFile(filepath.Join(path, name))
		check(err)

		// parse relevant data from manifest
		app := loadAppFromManifest(body)
		if app != nil {
			app.name = name[:len(name)-5]
			apps = append(apps, app)
		}
	}

	return apps
}

func loadBucketsFromDir(bucketsPath string) (buckets BucketMap) {
	bucketFileInfos, err := ioutil.ReadDir(bucketsPath)
	checkWith(err, "Buckets folder does not exist")

	var mutex sync.Mutex
	var wg sync.WaitGroup

	buckets = BucketMap{}
	for _, bucketFileInfo := range bucketFileInfos {
		wg.Add(1)
		go func(file os.FileInfo) {
			bucketName := file.Name()
			bucketPath := filepath.Join(bucketsPath, bucketName)
			appList := loadAppListFromDir(bucketPath)

			mutex.Lock()
			buckets[bucketPath] = appList
			mutex.Unlock()

			wg.Done()
		}(bucketFileInfo)
	}
	wg.Wait()
	return
}

func loadInstalledApps(appsPath string) (apps NameSourceMap) {
	appFileInfos, err := ioutil.ReadDir(appsPath)
	checkWith(err, "Apps folder does not exist")

	apps = NameSourceMap{}
	for _, appFileInfo := range appFileInfos {
		name := appFileInfo.Name()
		path := filepath.Join(appsPath, name)
		apps[name] = path
	}
	return
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
			body, err := ioutil.ReadAll(readCloser)
			readCloser.Close()
			check(err)

			app := loadAppFromManifest(body)
			if app != nil {
				_, filename := filepath.Split(innerPath)
				app.name = filename[:len(filename)-5] // remove ".json"
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
			app.name = strings.TrimSpace(row[namei])
			app.version = strings.TrimSpace(row[versioni])
			app.description = strings.TrimSpace(row[descriptioni])
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
		log.Println("Updating repository: " + url)
	}

	cmd := exec.Command(cmdline[0], cmdline[1:]...)
	cmd.Stdout = &stdout
	cmd.Stderr = &stdout // &stderr
	err = cmd.Run()
	checkWith(err, stdout.String())

	fmt.Println(stdout.String())

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
	cachePath = filepath.Join(getScoopHome(), "cache", category)
	checkWith(os.MkdirAll(cachePath, 0700), "Can't create cache directory: "+cachePath)
	return
}

func cacheGetUrl(cacheFilePath string, url string) (err error) {
	err = nil

	now := time.Now()
	f, err := os.Stat(cacheFilePath)
	cache_exists := !os.IsNotExist(err)

	if !cache_exists || g_CacheDuration < now.Sub(f.ModTime()) {
		fmt.Printf("Downloading: %s\n", url)
		response, err := http.Get(url)
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
		fmt.Printf(colorize("source.status", "using cache: %s\n"), cacheFilePath)
	}

	return
}

//=========================================================
// buckets.json: load json and the buckets it references
//=========================================================

// loads a buckets.json file into a map[name]sourceUrl
func loadNameSourceMapFromJsonFile(path string) (res NameSourceMap) {
	raw, err := ioutil.ReadFile(path)
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
