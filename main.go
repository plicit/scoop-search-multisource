package main

import (
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	//	"github.com/pkg/profile"
	"github.com/valyala/fastjson"
)

var version = "v0.1.20220728"

type SearchQuery struct {
	pattern *regexp.Regexp
	fields  []string
}

type SourceRef struct {
	cond string
	kind string
	path string
}

type SourceRefs []SourceRef

type AppInfo struct {
	name        string
	version     string
	description string
	homepage    string
	bins        []string
}

type AppList = []*AppInfo
type BucketMap = map[string]AppList
type NameSourceMap = map[string]string

type BucketsMatch struct {
	buckets BucketMap
	numApps int
}

func NewBucketsMatch() *BucketsMatch {
	return &BucketsMatch{make(BucketMap), 0}
}

type SearchState struct {
	args               *parsedArgs
	query              *SearchQuery
	sources            SourceRefs
	numSourcesSearched int

	// Each Source has a corresponding BucketsMatch here:
	matchList     []*BucketsMatch
	numAppMatches int
}

type ScoopConfig struct {
	userHome   string
	configHome string // $env:XDG_CONFIG_HOME, "$env:USERPROFILE\.config"
	configFile string // "$configHome\scoop\config.json"
	scoopDir   string // $env:SCOOP, (get_config 'rootPath'), "$env:USERPROFILE\scoop"
	// Scoop global apps directory
	globalDir       string // $env:SCOOP_GLOBAL, (get_config 'globalPath'), "$env:ProgramData\scoop"
	cacheDir        string // $env:SCOOP_CACHE, (get_config 'cachePath'), "$scoopdir\cache"
	namedSourceRefs map[string]SourceRef
}

var g_State = new(SearchState)
var g_Config = new(ScoopConfig)

// load and search (filter) the given source, returning filtered bucket matches and the total number of app matches in those buckets
func (state *SearchState) SearchSource(src *SourceRef) (match *BucketsMatch, err error) {
	// load buckets based upon type of source
	buckets, err := loadBucketsFrom(src.kind, src.path)
	if err != nil {
		return nil, fmt.Errorf("unable to get buckets from source: %s", src)
	}

	match = filterBuckets(state.query, buckets)
	match.buckets = renameBucketsToKnownNames(match.buckets, "/")

	fmt.Printf(colorize("source.summary", "- %d apps matched in %d/%d buckets\n\n"), match.numApps, len(match.buckets), len(buckets))

	state.matchList = append(state.matchList, match)
	state.numAppMatches += match.numApps

	return match, nil
}

func (state *SearchState) Run(args *parsedArgs) error {
	state.args = args
	state.query = &args.query
	state.sources = args.sources
	// g_State.index = 0
	// g_State.numAppMatches = 0
	// g_State.matches

	merge := args.merge

	divider := colorize("divider", "____________________\n")

	if args.debug {
		fmt.Printf(colorize("debug", "  QUERY")+": %s\n", state.query.pattern)
		fmt.Printf(colorize("debug", " FIELDS")+": %s\n", strings.Join(state.query.fields, ","))
		fmt.Printf(colorize("debug", "SOURCES")+": %v\n", state.sources)
		fmt.Printf(colorize("debug", " COLORS")+": %v\n", state.args.colors.StringAll(true))
	}

	for index, src := range state.sources {
		if src.cond == "if0" && state.numAppMatches > 0 {
			continue
		}

		fmt.Print(divider)
		fmt.Printf(colorize("source.header", "#%d Searching %s [%s] %s\n"), index+1, src.cond, src.kind, src.path)

		match, _ := state.SearchSource(&src)
		state.numSourcesSearched += 1
		if !merge {
			printResults(match.buckets, args.linelen)
		}
	}

	// calculate the total number of buckets matching from each Source
	numBuckets := 0
	for _, match := range state.matchList {
		numBuckets += len(match.buckets)
	}

	fmt.Print(divider)
	fmt.Printf(colorize("totals", "TOTAL: %d apps matched in %d buckets from %d sources\n\n"), state.numAppMatches, numBuckets, state.numSourcesSearched)

	if merge {
		fmt.Printf("MERGED RESULTS:\n\n")
		merged := NewBucketsMatch()
		for _, match := range state.matchList {
			for name, appList := range match.buckets {
				merged.buckets[name] = appList
				merged.numApps += len(appList)
			}
		}
		printResults(merged.buckets, args.linelen)
	}

	return nil
}

// resolves paths to scoop folders and loads config
func loadScoopConfig() (err error) {
	//configHome string // $env:XDG_CONFIG_HOME, "$env:USERPROFILE\.config"
	//configFile string // "$configHome\scoop\config.json"
	//scoopDir string // $env:SCOOP, (get_config 'rootPath'), "$env:USERPROFILE\scoop"
	//// Scoop global apps directory
	//globalDir string // $env:SCOOP_GLOBAL, (get_config 'globalPath'), "$env:ProgramData\scoop"
	//cacheDir string // $env:SCOOP_CACHE, (get_config 'cachePath'), "$scoopdir\cache"

	// Scoop root directory
	// $scoopdir = $env:SCOOP, (get_config 'rootPath'), "$env:USERPROFILE\scoop" | Where-Object { -not [String]::IsNullOrEmpty($_) } | Select-Object -First 1
	// FirstPathThatExists?

	userHome, err := os.UserHomeDir()
	checkWith(err, "Could not determine home dir")
	g_Config.userHome = strings.ReplaceAll(userHome, "/", "\\")

	// $configHome = $env:XDG_CONFIG_HOME, "$env:USERPROFILE\.config" | Select-Object -First 1
	// $configFile = "$configHome\scoop\config.json"

	if value, ok := os.LookupEnv("XDG_CONFIG_HOME"); ok {
		g_Config.configHome = value
	} else {
		g_Config.configHome = filepath.Join(g_Config.userHome, ".config")
	}
	g_Config.configHome = strings.ReplaceAll(g_Config.configHome, "/", "\\")

	//fmt.Println("*** configHome:", configHome)

	// parse scoop config.json for rootPath
	g_Config.configFile = filepath.Join(g_Config.configHome, "scoop", "config.json")
	//fmt.Println("*** configFile:", configFile)

	//{
	//	"lastupdate": "2022-06-20T13:38:23.6926110-05:00",
	//	"SCOOP_REPO": "https://github.com/ScoopInstaller/Scoop",
	//	"SCOOP_BRANCH": "master"
	//}
	body, err := ioutil.ReadFile(g_Config.configFile)
	if err == nil && len(body) > 0 {
		var parser fastjson.Parser
		js, _ := parser.ParseBytes(body)
		g_Config.scoopDir = string(js.GetStringBytes("rootPath"))
		g_Config.globalDir = string(js.GetStringBytes("globalPath"))
		g_Config.cacheDir = string(js.GetStringBytes("cachePath"))
	}

	if value, ok := os.LookupEnv("SCOOP"); ok {
		g_Config.scoopDir = value
		//fmt.Println("*** SCOOP ENV!:", scoopHome)
	}

	// if it's not in the config file
	if g_Config.scoopDir == "" {
		g_Config.scoopDir = filepath.Join(g_Config.userHome, "scoop")
		//fmt.Println("*** default userHome scoopHome:", scoopHome)
	}

	g_Config.scoopDir = strings.ReplaceAll(g_Config.scoopDir, "/", "\\")

	//globalDir string // $env:SCOOP_GLOBAL, (get_config 'globalPath'), "$env:ProgramData\scoop"
	globalDir := os.Getenv("SCOOP_GLOBAL")
	if globalDir != "" {
		g_Config.globalDir = globalDir
	}
	if g_Config.globalDir == "" {
		g_Config.globalDir = filepath.Join(os.Getenv("ProgramData"), "scoop")
	}

	g_Config.globalDir = strings.ReplaceAll(g_Config.globalDir, "/", "\\")

	//cacheDir string // $env:SCOOP_CACHE, (get_config 'cachePath'), "$scoopdir\cache"
	cacheDir := os.Getenv("SCOOP_CACHE")
	if cacheDir != "" {
		g_Config.cacheDir = cacheDir
	}
	if g_Config.cacheDir == "" {
		g_Config.cacheDir = filepath.Join(g_Config.scoopDir, "cache")
	}

	g_Config.cacheDir = strings.ReplaceAll(g_Config.cacheDir, "/", "\\")

	g_Config.namedSourceRefs = map[string]SourceRef{}

	g_Config.namedSourceRefs["active"] = SourceRef{"", "buckets", filepath.Join(g_Config.scoopDir, "buckets")}
	g_Config.namedSourceRefs["rasa"] = SourceRef{"", "html", "https://rasa.github.io/scoop-directory/by-score.html"}
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
	//defer profile.Start().Stop() // CPU profiling by default

	defer timeTrack(time.Now(), "Search")

	loadScoopConfig()

	initColorize()

	args := parseArgs()
	mergeColorMap(args.colors)

	// don't allow an empty query
	// if the user wants all apps, they can supply a dot or empty string "" which will have "(?i)" prepended to it and skip this
	if args.query.pattern.String() == "" {
		myUsage()
	} else {
		// merge semantic color names into color values

		g_State.Run(args)
	}

	// exit with status code
	if g_State.numAppMatches == 0 {
		os.Exit(1)
	}
}

// Search by filtering apps and buckets
func filterApp(query *SearchQuery, app *AppInfo) bool {
	found := false
	//	if strings.Contains(strings.ToLower(app.name), opt) {
	for _, field := range query.fields {
		switch field {
		case "name":
			if query.pattern.MatchString(app.name) {
				app.bins = nil // ignore bin if name matches
				found = true
			}
		case "bins":
			var bins []string
			for _, bin := range app.bins {
				bin = filepath.Base(bin)
				//			if strings.Contains(strings.ToLower(strings.TrimSuffix(bin, filepath.Ext(bin))), opt) {
				if query.pattern.MatchString(strings.TrimSuffix(bin, filepath.Ext(bin))) {
					bins = append(bins, bin)
					found = true
				}
			}
			app.bins = bins
		case "description":
			if query.pattern.MatchString(app.description) {
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
		return strings.ToLower(strings.ReplaceAll(matches[i].name, "-", "")) <= strings.ToLower(strings.ReplaceAll(matches[j].name, "-", ""))
	})

	return
}

// uses query to filter buckets into a new BucketMap of matches
func filterBuckets(query *SearchQuery, buckets BucketMap) (match *BucketsMatch) {
	match = NewBucketsMatch()
	match.numApps = 0
	match.buckets = BucketMap{}
	for source, apps := range buckets {
		apps = filterAppList(query, apps)
		if len(apps) > 0 {
			match.buckets[source] = apps
			match.numApps += len(apps)
		}
	}
	return
}

// uses %SCOOP%\apps\scoop\current\buckets.json to rename bucket source paths/urls to known names
func renameBucketsToKnownNames(buckets BucketMap, prefix string) (res BucketMap) {
	// we will remove any local buckets path prefix
	bucketsPath := g_Config.scoopDir + "\\buckets\\"

	// load known bucket name:source from buckets.json
	bucketsJsonFile := filepath.Join(g_Config.scoopDir, "apps", "scoop", "current", "buckets.json")
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
	loadInstalledApps(g_Config.scoopDir+"\\apps", &userInstalled)

	var globalInstalled = NameSourceMap{}
	loadInstalledApps(g_Config.globalDir+"\\apps", &globalInstalled)

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
				if _, exists := userInstalled[m.name]; exists {
					color = "app.name.installed"
					prefix = " ** "
				} else if _, exists := globalInstalled[m.name]; exists {
					color = "app.name.installed"
					prefix = " G* "
				}

				line = colorize(color, prefix+m.name)
				line += " (" + colorize("app.version", m.version) + ")"

				if len(m.bins) != 0 {
					// display.WriteString(" --> includes '")
					// bins := strings.Join(m.bins, ",")
					bins := m.bins[0]
					line += " [" + bins + "]"
				}
				display.WriteString(line)

				remainder := MaxInt(0, linelen-len(line)-2)
				if len(m.description) != 0 && remainder > 0 {
					display.WriteString(": ")
					display.WriteString(colorize("app.description", m.description[:MinInt(remainder, len(m.description))]))
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
