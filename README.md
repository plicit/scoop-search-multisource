# scoop-search-multisource

[![](https://goreportcard.com/badge/github.com/plicit/scoop-search-multisource)](https://goreportcard.com/report/github.com/plicit/scoop-search-multisource)
[![](https://github.com/plicit/scoop-search-multisource/workflows/ci/badge.svg)](https://github.com/plicit/scoop-search-multisource/actions)

- Searches local active buckets and remote rasa html directory by default
- 47x faster than `scoop search`
- colored results
- customization through command line switches

## Example
```
> scoops.exe "qr"
____________________
#1 Searching  [buckets] c:\_scoop\buckets
- 3 apps matched in 2/12 buckets

____________________
#2 Searching  [html] https://rasa.github.io/scoop-directory/by-score.html
using cache: c:\_scoop\cache\buckets\https%3A%2F%2Frasa.github.io%2Fscoop-directory%2Fby-score.html.html
- 8 apps matched in 7/699 buckets

____________________
TOTAL: 11 apps matched in 9 buckets from 2 sources

MERGED RESULTS:

'/extras' bucket:
    renderdoc (1.18) [qrenderdoc.exe]: A stand-alone graphics debugging tool

'/main' bucket:
 ** qr (1.0.0): A command line for generating QR-code from console.
    qrcp (0.8.4): Transfers files over wifi from computer to mobile device by scanning a QR code without le

'https://github.com/404NetworkError/scoop-bucket' bucket:
    qrencode (4.1.1): Encode input data in a QR Code and save as a PNG image [<em>Failed validating requir

'https://github.com/Casuor/AkiWinApps' bucket:
    qrcp (0.8.4): Transfers files over wifi from computer to mobile device by scanning a QR code without le

'https://github.com/ChandlerVer5/scoop-fruit' bucket:
    qr-filetransfer (0.1): [<em>Failed validating 'required' in schema:</em>]

'https://github.com/DoveBoy/Scoop-Bucket' bucket:
    qrcp (0.8.4): Transfers files over wifi from computer to mobile device by scanning a QR code without le

'https://github.com/wangzq/scoop-bucket' bucket:
    portqry (2.0): [<em>Failed validating 'required' in schema:</em>]

'https://github.com/warexify/scoop-edk2-buildtools' bucket:
    reqrypt (1.4.1): Request Encryption

2022/02/22 18:40:32 Search took 467.126ms
```

## Switches

```
> scoops -help
scoop-search-multisource.exe : Searches Scoop buckets: local, remote, zip, html

VERSION: 0.1.20220411
   HOME: https://github.com/plicit/scoop-search-multisource

  ALIAS: scoops.exe
  USAGE: scoops.exe [OPTIONS] <search-term-or-regexp>
   NOTE: search-term is case-insensitive.  Prefix with "(?-i)" for case-sensitive.  See https://pkg.go.dev/regexp/syntax

EXAMPLE: scoops.exe -debug -merge=0 -source :active -source "if0: :rasa" -fields "name,bins,description" "\bqr\b"

OPTIONS:

  -cache float
        cache duration in days. (default 1)
  -colors value
        colormap for output. "none" deletes the colormap. (default debug=light_red;app.name=yellow;app.name.installed=light_green;source.header=light_cyan;source.summary=source.status;totals=light_cyan)
  -debug
        print debug info (query, fields, sources)
  -fields string
        app manifest fields to search: name,bins,description (default "name,bins")
  -hook
        print posh hook code to integrate with scoop
  -linelen int
        max line length for results (trims description) (default 120)
  -merge
        merge the results from all sources into a single output (avoids duplicates)
  -source value
        a specific source to search. (multiple allowed)

        SOURCE FORMAT: "<if0:> [<bucket|buckets|html>] <:active|:rasa or path/url>"
          if0: -- only use the source as a fallback if there were 0 previous matches

        EXAMPLES:
          scoops.exe -source "mybucket.zip" -source "if0: :rasa" python
          scoops.exe -source "[html] https://rasa.github.io/scoop-directory/by-score.html" actools
          scoops.exe -source "[bucket] https://github.com/ScoopInstaller/Versions" python
          scoops.exe -source "%USERPROFILE%\scoop\buckets\main" python
```

## Installation

```
> scoop install "https://raw.githubusercontent.com/plicit/scoop-search-multisource/master/scoop-search-multisource.json"
```

## Powershell Hook

If you use Powershell, then instead of using `scoop-search-multisource.exe <term>` or the alias `scoops.exe <term>`, you can setup a hook that will run `scoop-search-multisource.exe` whenever you use native `scoop search`

Add this to your Powershell profile (usually located at `$PROFILE`)

```
PS > Invoke-Expression (&scoop-search-multisource --hook)
```

The hook is basically:

```ps1
function scoop { if ($args[0] -eq "search") { scoop-search-multisource.exe @($args | Select-Object -Skip 1) } else { scoop.ps1 @args } }
```

## Avoiding Long Delays in Execution

If you periodically get long delays when searching your active buckets, then it may be that Windows Defender (Antimalware Service Executable msmpeng.exe) is re-scanning them on-demand before allowing `scoops` to read them.

To skip such security scanning, you could add your [active buckets directory](https://github.com/ScoopInstaller/Scoop/wiki/Scoop-Folder-Layout) as an ExclusionPath so that Windows Defender excludes it from scanning.  For example, this excludes the buckets in the default locations as well as if the SCOOP and SCOOP_GLOBAL vars are set:

```ps1
# Exclude your active buckets directories
# default locations:
Add-MpPreference -ExclusionPath "${env:ProgramData}\scoop\buckets", "${env:USERPROFILE}\scoop\buckets" -Force
# OR if SCOOP and SCOOP_GLOBAL env variables are set:
Add-MpPreference -ExclusionPath "${env:SCOOP}\buckets", "${env:SCOOP_GLOBAL}\buckets" -Force

# View the current paths excluded
# via powershell:
$(Get-MpPreference).ExclusionPath
# OR via cmd:
reg query "HKEY_LOCAL_MACHINE\SOFTWARE\Microsoft\Windows Defender\Exclusions\Paths" /s /reg:64
```

## Benchmarks

`scoop-search-multisource` is about 47 times faster than the PowerShell `scoop search`.  Tested using [hyperfine](https://github.com/sharkdp/hyperfine):

```
PS > hyperfine --warmup 1 'scoop-search-multisource -source :active google' 'scoop-search-multisource -source :rasa google' 'scoop search google'
Benchmark 1: scoop-search-multisource -source :active google
  Time (mean ± σ):     208.0 ms ±   7.1 ms    [User: 4.2 ms, System: 3.9 ms]
  Range (min … max):   200.0 ms … 218.3 ms    13 runs

Benchmark 2: scoop-search-multisource -source :rasa google
  Time (mean ± σ):     323.1 ms ±  13.2 ms    [User: 3.3 ms, System: 9.5 ms]
  Range (min … max):   310.9 ms … 349.1 ms    10 runs

Benchmark 3: scoop search google
  Time (mean ± σ):      9.887 s ±  0.270 s    [User: 0.008 s, System: 0.015 s]
  Range (min … max):    9.484 s … 10.249 s    10 runs

Summary
  'scoop-search-multisource -source :active google' ran
    1.55 ± 0.08 times faster than 'scoop-search-multisource -source :rasa google'
   47.54 ± 2.08 times faster than 'scoop search google'
```

## Related projects

- [mertd/shovel-data](https://github.com/mertd/shovel-data) - A script that checks out all supported scoop buckets and collects the manifests in one searchable json file.
- [rasa/scoop-directory](https://github.com/rasa/scoop-directory) - Scoop directory updated daily
- [shilangyu/scoop-search](https://github.com/shilangyu/scoop-search) - Original scoop-search in Go
- [zhoujin7/crawl-scoop-directory](https://github.com/zhoujin7/crawl-scoop-directory) - creates an sqlite db from rasa
- [zhoujin7/scoop-search](https://github.com/zhoujin7/scoop-search) - web front end to sqlite db

## Provenance

Thanks to @shilangyu's [scoop-search](https://github.com/shilangyu/scoop-search) on which this fork is based.  I implemented a vast superset of [scoop-search issue #6](https://github.com/shilangyu/scoop-search/issues/6).

