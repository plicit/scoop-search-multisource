package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"golang.org/x/exp/slices"

	"github.com/valyala/fastjson"
	////"github.com/pkg/profile"
	//"github.com/mmcloughlin/profile"
	//_ "github.com/mattn/go-sqlite3"
	//_ "github.com/pocketbase/dbx"
	// _ "github.com/mutecomm/go-sqlcipher/v4"
)

var g_Version = "v0.1.20230520"

type SearchQuery struct {
	Pattern *regexp.Regexp
	Fields  []string
}

type SourceRef struct {
	Cond string
	Kind string
	Path string
}

type SourceRefs []SourceRef

type AppInfo struct {
	Name        string
	Version     string
	Description string
	Homepage    string
	Bins        []string
}

type AppList = []*AppInfo
type BucketMap = map[string]AppList
type NameSourceMap = map[string]string

type BucketsMatch struct {
	Buckets BucketMap
	NumApps int
}

func NewBucketsMatch() *BucketsMatch {
	return &BucketsMatch{make(BucketMap), 0}
}

type SearchState struct {
	Args               *ParsedArgs
	Query              *SearchQuery
	Sources            SourceRefs
	NumSourcesSearched int

	// Each Source has a corresponding BucketsMatch here:
	MatchList     []*BucketsMatch
	NumAppMatches int
}

type ScoopConfig struct {
	UserHome        string
	ConfigHome      string // $env:XDG_CONFIG_HOME, "$env:USERPROFILE\.config"
	ScoopConfigFile string // "$configHome\scoop\config.json"
	ScoopDir        string // $env:SCOOP, (get_config 'root_path'), "$env:USERPROFILE\scoop"
	// Scoop global apps directory
	ScoopGlobalDir  string // $env:SCOOP_GLOBAL, (get_config 'globalPath'), "$env:ProgramData\scoop"
	ScoopCacheDir   string // $env:SCOOP_CACHE, (get_config 'cachePath'), "$scoopdir\cache"
	ScoopProxy      string
	NamedSourceRefs map[string]SourceRef
}

var DEBUG bool
var g_State = new(SearchState)
var g_Config = new(ScoopConfig)

// load and search (filter) the given source, returning filtered bucket matches and the total number of app matches in those buckets
func (state *SearchState) SearchSource(src *SourceRef) (match *BucketsMatch, err error) {
	// load buckets based upon type of source
	buckets, err := loadBucketsFrom(src.Kind, src.Path)
	if err != nil {
		return nil, fmt.Errorf("unable to get buckets from source: %s", src)
	}

	match = filterBuckets(state.Query, buckets)
	match.Buckets = renameBucketsToKnownNames(match.Buckets, "/")

	fmt.Printf(colorize("source.summary", "- %d apps matched in %d/%d buckets\n\n"), match.NumApps, len(match.Buckets), len(buckets))

	state.MatchList = append(state.MatchList, match)
	state.NumAppMatches += match.NumApps

	return match, nil
}

func (state *SearchState) Run(args *ParsedArgs) error {
	state.Args = args
	state.Query = &args.query
	state.Sources = args.sources
	// g_State.index = 0
	// g_State.numAppMatches = 0
	// g_State.matches

	merge := args.merge

	divider := colorize("divider", "____________________\n")

	if DEBUG {
		fmt.Printf(colorize("debug", "VERSION")+": %s\n", g_Version)
		fmt.Printf(colorize("debug", "  QUERY")+": %s\n", state.Query.Pattern)
		fmt.Printf(colorize("debug", " FIELDS")+": %s\n", strings.Join(state.Query.Fields, ","))
		fmt.Printf(colorize("debug", "SOURCES")+": %v\n", state.Sources)
		fmt.Printf(colorize("debug", " COLORS")+": %v\n", state.Args.colors.StringAll(true))

		if b, err := json.Marshal(g_Config); err == nil {
			fmt.Printf(colorize("debug", " CONFIG")+": %s\n", string(b))
		} else {
			fmt.Println(err)
		}

		//v := reflect.ValueOf(s)
		//typeOfS := v.Type()
		//for i := 0; i < v.NumField(); i++ {
		//	fmt.Printf("Field: %s\tValue: %v\n", typeOfS.Field(i).Name, v.Field(i).Interface())
		//}
	}

	for index, src := range state.Sources {
		if src.Cond == "if0" && state.NumAppMatches > 0 {
			continue
		}

		fmt.Print(divider)
		fmt.Printf(colorize("source.header", "#%d Searching %s [%s] %s\n"), index+1, src.Cond, src.Kind, src.Path)

		match, _ := state.SearchSource(&src)
		state.NumSourcesSearched += 1
		if !merge {
			printResults(match.Buckets, args.linelen)
		}
	}

	// calculate the total number of buckets matching from each Source
	numBuckets := 0
	for _, match := range state.MatchList {
		numBuckets += len(match.Buckets)
	}

	fmt.Print(divider)
	fmt.Printf(colorize("totals", "TOTAL: %d apps matched in %d buckets from %d sources\n\n"), state.NumAppMatches, numBuckets, state.NumSourcesSearched)

	if merge {
		fmt.Printf("MERGED RESULTS:\n\n")
		merged := NewBucketsMatch()
		for _, match := range state.MatchList {
			for name, appList := range match.Buckets {
				merged.Buckets[name] = appList
				merged.NumApps += len(appList)
			}
		}
		printResults(merged.Buckets, args.linelen)
	}

	return nil
}

//func debug(arg ...interface{}) {
//	if DEBUG {
//		fmt.Println(arg...) // forward it here
//	}
//}

// resolves paths to scoop folders and loads config
func loadScoopConfig() (err error) {
	// %SCOOP%\apps\scoop\current\lib\core.ps1

	//configHome string // $env:XDG_CONFIG_HOME, "$env:USERPROFILE\.config"
	//configFile string // "$configHome\scoop\config.json"
	//scoopDir string // $env:SCOOP, (get_config 'root_path'), "$env:USERPROFILE\scoop"
	//// Scoop global apps directory
	//globalDir string // $env:SCOOP_GLOBAL, (get_config 'globalPath'), "$env:ProgramData\scoop"
	//cacheDir string // $env:SCOOP_CACHE, (get_config 'cachePath'), "$scoopdir\cache"

	// Scoop root directory
	// $scoopdir = $env:SCOOP, (get_config 'root_path'), "$env:USERPROFILE\scoop" | Where-Object { -not [String]::IsNullOrEmpty($_) } | Select-Object -First 1
	// FirstPathThatExists?

	//g_Config.ConfigFrom := make([]string, 0)

	userHome, err := os.UserHomeDir()
	checkWith(err, "Could not determine user's home dir")
	g_Config.UserHome = strings.ReplaceAll(userHome, "/", "\\")

	// $configHome = $env:XDG_CONFIG_HOME, "$env:USERPROFILE\.config" | Select-Object -First 1
	// $configFile = "$configHome\scoop\config.json"

	if value, ok := os.LookupEnv("XDG_CONFIG_HOME"); ok {
		g_Config.ConfigHome = value
		//		if DEBUG {
		//			fmt.Printf("$env:XDG_CONFIG_HOME=%s\n", g_Config.ConfigHome)
		//		}
	} else {
		g_Config.ConfigHome = filepath.Join(g_Config.UserHome, ".config")
		//		if DEBUG {
		//			fmt.Printf("$env:USERPROFILE=%s\n", g_Config.ConfigHome)
		//		}
	}
	g_Config.ConfigHome = strings.ReplaceAll(g_Config.ConfigHome, "/", "\\")

	//fmt.Println("*** configHome:", configHome)

	// parse scoop config.json for root_path
	g_Config.ScoopConfigFile = filepath.Join(g_Config.ConfigHome, "scoop", "config.json")
	//fmt.Println("*** configFile:", configFile)

	//{
	//	"lastupdate": "2022-06-20T13:38:23.6926110-05:00",
	//	"SCOOP_REPO": "https://github.com/ScoopInstaller/Scoop",
	//	"SCOOP_BRANCH": "master"
	//}
	body, err := os.ReadFile(g_Config.ScoopConfigFile)
	if err == nil && len(body) > 0 {
		var parser fastjson.Parser
		js, _ := parser.ParseBytes(body)
		g_Config.ScoopDir = string(js.GetStringBytes("root_path"))
		g_Config.ScoopGlobalDir = string(js.GetStringBytes("global_path"))
		g_Config.ScoopCacheDir = string(js.GetStringBytes("cache_path"))
		g_Config.ScoopProxy = string(js.GetStringBytes("proxy"))
		if DEBUG {
			fmt.Printf("Loaded ScoopConfigFile=%s\n", g_Config.ScoopConfigFile)
			fmt.Printf("ScoopDir=%s\n", g_Config.ScoopDir)
		}
	}

	// https://github.com/42wim/scoop-bucket/blob/master/.appveyor.yml
	// environment:
	// 	 SCOOP: C:\projects\scoop
	// 	 SCOOP_HOME: C:\projects\scoop\apps\scoop\current

	// scoopDir string // $env:SCOOP, (get_config 'rootPath'), "$env:USERPROFILE\scoop"
	// env overrides config!
	if value, ok := os.LookupEnv("SCOOP"); ok {
		g_Config.ScoopDir = value
		if DEBUG {
			fmt.Printf("ScoopDir=$env:SCOOP=%s\n", g_Config.ScoopDir)
		}
	}

	// if it's not in the env OR config file
	if g_Config.ScoopDir == "" {
		g_Config.ScoopDir = filepath.Join(g_Config.UserHome, "scoop")
		if DEBUG {
			fmt.Printf("ScoopDir=(no config)=%s\n", g_Config.ScoopDir)
		}
	}

	g_Config.ScoopDir = strings.ReplaceAll(g_Config.ScoopDir, "/", "\\")

	//globalDir string // $env:SCOOP_GLOBAL, (get_config 'globalPath'), "$env:ProgramData\scoop"
	globalDir := os.Getenv("SCOOP_GLOBAL")
	if globalDir != "" {
		g_Config.ScoopGlobalDir = globalDir
		if DEBUG {
			fmt.Printf("ScoopGlobalDir=$env:SCOOP_GLOBAL=%s\n", g_Config.ScoopGlobalDir)
		}
	}
	if g_Config.ScoopGlobalDir == "" {
		g_Config.ScoopGlobalDir = filepath.Join(os.Getenv("ProgramData"), "scoop")
		if DEBUG {
			fmt.Printf("ScoopGlobalDir=(no config)=%s\n", g_Config.ScoopGlobalDir)
		}
	}

	g_Config.ScoopGlobalDir = strings.ReplaceAll(g_Config.ScoopGlobalDir, "/", "\\")

	//cacheDir string // $env:SCOOP_CACHE, (get_config 'cachePath'), "$scoopdir\cache"
	cacheDir := os.Getenv("SCOOP_CACHE")
	if cacheDir != "" {
		g_Config.ScoopCacheDir = cacheDir
		//		if DEBUG {
		//			fmt.Printf("ScoopCacheDir=$env:SCOOP_CACHE=%s\n", g_Config.ScoopCacheDir)
		//		}
	}
	if g_Config.ScoopCacheDir == "" {
		g_Config.ScoopCacheDir = filepath.Join(g_Config.ScoopDir, "cache")
		//		if DEBUG {
		//			fmt.Printf("ScoopCacheDir=%s\n", g_Config.ScoopCacheDir)
		//		}
	}

	g_Config.ScoopCacheDir = strings.ReplaceAll(g_Config.ScoopCacheDir, "/", "\\")

	g_Config.NamedSourceRefs = map[string]SourceRef{}

	// must be after loading config
	g_Config.NamedSourceRefs["active"] = SourceRef{"", "buckets", filepath.Join(g_Config.ScoopDir, "buckets")}
	g_Config.NamedSourceRefs["rasa"] = SourceRef{"", "html", "https://rasa.github.io/scoop-directory/by-score.html"}
	//	"rasa":   {"if0", "html", "https://rasa.github.io/scoop-directory/by-score.html"},

	//fmt.Println(g_Config)
	//os.Exit(1)
	return
}

func timeTrack(start time.Time, name string) {
	elapsed := time.Since(start)
	log.Printf("%s took %s\n", name, elapsed)
}

func main() {
	// uncomment to profile:
	//defer profile.Start(profile.CPUProfile, profile.ProfilePath(".")).Stop()
	// https://github.com/mmcloughlin/profile
	//defer profile.Start(
	//	profile.AllProfiles,
	//	profile.ConfigEnvVar("GO_PERF_PROFILE"),
	//).Stop()

	defer timeTrack(time.Now(), "Search")

	DEBUG = slices.Contains(os.Args, "-debug")

	// must be before parsing the command line due to g_Config.NamedSourceRefs
	loadScoopConfig()

	initColorize()

	args := parseArgs()
	mergeColorMap(args.colors)

	// don't allow an empty query
	// if the user wants all apps, they can supply a dot or empty string "" which will have "(?i)" prepended to it and skip this
	if args.query.Pattern.String() == "" {
		myUsage()
	} else {
		g_State.Run(args)
	}

	// exit with status code
	if g_State.NumAppMatches == 0 {
		os.Exit(1)
	}
}

// Search by filtering apps and buckets
func filterApp(query *SearchQuery, app *AppInfo) bool {
	found := false
	//	if strings.Contains(strings.ToLower(app.name), opt) {
	for _, field := range query.Fields {
		switch field {
		case "name":
			if query.Pattern.MatchString(app.Name) {
				app.Bins = nil // ignore bin if name matches
				found = true
			}
		case "bins":
			var bins []string
			for _, bin := range app.Bins {
				bin = filepath.Base(bin)
				//			if strings.Contains(strings.ToLower(strings.TrimSuffix(bin, filepath.Ext(bin))), opt) {
				if query.Pattern.MatchString(strings.TrimSuffix(bin, filepath.Ext(bin))) {
					bins = append(bins, bin)
					found = true
				}
			}
			app.Bins = bins
		case "description":
			if query.Pattern.MatchString(app.Description) {
				found = true
			}
		}
	}
	return found
}

func filterAppList(query *SearchQuery, apps AppList) (matches AppList) {
	matches = AppList{}
	for _, app := range apps {
		if filterApp(query, app) {
			matches = append(matches, app)
		}
	}

	// sort the apps by name
	sort.SliceStable(matches, func(i, j int) bool {
		// case insensitive comparison where hyphens are ignored
		return strings.ToLower(strings.ReplaceAll(matches[i].Name, "-", "")) <= strings.ToLower(strings.ReplaceAll(matches[j].Name, "-", ""))
	})

	return
}

// uses query to filter buckets into a new BucketMap of matches
func filterBuckets(query *SearchQuery, buckets BucketMap) (match *BucketsMatch) {
	match = NewBucketsMatch()
	match.NumApps = 0
	match.Buckets = BucketMap{}
	for source, apps := range buckets {
		apps = filterAppList(query, apps)
		if len(apps) > 0 {
			match.Buckets[source] = apps
			match.NumApps += len(apps)
		}
	}
	return
}

// uses %SCOOP%\apps\scoop\current\buckets.json to rename bucket source paths/urls to known names
func renameBucketsToKnownNames(buckets BucketMap, prefix string) (res BucketMap) {
	// we will remove any local buckets path prefix
	bucketsPath := g_Config.ScoopDir + "\\buckets\\"

	// load known bucket name:source from buckets.json
	bucketsJsonFile := filepath.Join(g_Config.ScoopDir, "apps", "scoop", "current", "buckets.json")
	bucketsByName := loadNameSourceMapFromJsonFile(bucketsJsonFile)
	bucketsBySource := map[string]string{}

	// create a reverse map that is source:name
	for name, source := range bucketsByName {
		bucketsBySource[source] = name
	}

	// create a bucket map that uses the known names for known sources
	res = BucketMap{}
	for source, applist := range buckets {
		if name, ok := bucketsBySource[source]; ok {
			res[prefix+name] = applist
		} else {
			// if source was a local bucket in $SCOOP/buckets, then trim the directory
			trimmed := strings.TrimPrefix(source, bucketsPath)
			if trimmed != source {
				source = prefix + trimmed
			}
			res[source] = applist
		}
	}

	return res
}

// print the given buckets as search results
func printResults(buckets BucketMap, linelen int) (anyMatches bool) {
	// sort by bucket names
	//fmt.Printf("colors=%v\n", g_State.args.colors)
	entries := 0
	sortedKeys := make([]string, 0, len(buckets))
	for k := range buckets {
		entries += len(buckets[k])
		sortedKeys = append(sortedKeys, k)
	}
	sort.Strings(sortedKeys)

	var userInstalled = NameSourceMap{}
	err := loadInstalledApps(g_Config.ScoopDir+"\\apps", &userInstalled)
	if err != nil {
		log.Println("loadInstalledApps: Apps path does not exist: ", err)
	}

	var globalInstalled = NameSourceMap{}
	loadInstalledApps(g_Config.ScoopGlobalDir+"\\apps", &globalInstalled)
	// ignore if global apps path doesn't exist

	// reserve additional space assuming each variable string has length 1. Will save time on initial allocations
	var display strings.Builder
	display.Grow((len(sortedKeys)*12 + entries*11))

	for _, k := range sortedKeys {
		v := buckets[k]

		if len(v) > 0 {
			anyMatches = true

			line := ""
			line = fmt.Sprintf(colorize("bucket.header", "'%s' bucket:\n"), k)
			display.WriteString(line)

			for _, m := range v {
				prefix := "    "
				color := "app.name"
				if _, exists := userInstalled[m.Name]; exists {
					color = "app.name.installed"
					prefix = " ** "
				} else if _, exists := globalInstalled[m.Name]; exists {
					color = "app.name.installed"
					prefix = " G* "
				}

				line = colorize(color, prefix+m.Name)
				line += " (" + colorize("app.version", m.Version) + ")"

				if len(m.Bins) != 0 {
					// display.WriteString(" --> includes '")
					// bins := strings.Join(m.bins, ",")
					bins := m.Bins[0]
					line += " [" + bins + "]"
				}
				display.WriteString(line)

				remainder := MaxInt(0, linelen-len(line)-2)
				if len(m.Description) != 0 && remainder > 0 {
					display.WriteString(": ")
					display.WriteString(colorize("app.description", m.Description[:MinInt(remainder, len(m.Description))]))
				}
				display.WriteString("\n")
			}
			display.WriteString("\n")
		}
	}

	if !anyMatches {
		display.WriteString("No matches found.\n")
	}

	os.Stdout.WriteString(display.String())
	return
}
