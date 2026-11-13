# Home screen memory investigation — 2026-11-13

Measurements were taken on Windows using WebView2 Runtime 157.0.4341.46. Phoenix was built with Go 1.27.3, `-tags production -trimpath -buildvcs=false -ldflags="-w -s -H windowsgui"`. Each launch used isolated, empty application-data and test-profile directories. Samples were collected 20 seconds after launch using a Go probe and Windows `GetProcessMemoryInfo` (`PrivateWorkingSetSize`), for the app and its descendant processes. Windows can trim resident pages, so these figures depend on focus, memory pressure, and timing.

| Scenario | Private resident memory, MiB | GPU process, MiB | Go app, MiB |
| --- | ---: | ---: | ---: |
| Phoenix, GPU enabled, first run | 177.2 | 62.4 | 46.8 |
| Phoenix, GPU disabled, first run | 95.9 | 9.8 | 14.7 |
| Phoenix, GPU disabled, repeat | 92.6 | 9.5 | 14.0 |
| Phoenix, GPU enabled, repeat | 149.5 | 72.3 | 14.1 |
| Phoenix, GPU disabled, idle CPU run | 87.6 | 9.5 | 14.4 |
| Phoenix, GPU enabled, idle CPU run | 148.7 | 71.5 | 14.1 |
| Minimal Go/Wails window, GPU disabled | 99.0 | 9.1 | 38.0 |
| Minimal Go/Wails window, GPU enabled | 107.0 | 47.2 | 5.6 |

The minimal program opened a simple HTML page in a 1280 × 800 Wails window using its own WebView2 profile. It had no Phoenix services or interface. Its window configuration and resident memory trimming differ from Phoenix, so its totals are indicative rather than a precise subtraction. The large difference in app working set between minimal runs also shows why the totals must not be read as a controlled per-feature attribution.

In the final Phoenix runs, total CPU time across the process tree over ten seconds of idle sampling was approximately 0.016 seconds with GPU disabled and 0.109 seconds with GPU enabled. These brief idle measurements do not predict CPU cost, smoothness, or battery use during scrolling, video, page rendering, or agent work.

An isolated instrumented production build captured a Go heap profile after 20 seconds and a forced garbage collection. With the default sampling rate it estimated approximately 5.1 MiB; repeating with `GODEBUG=memprofilerate=1` recorded approximately 1.61 MiB of live heap. The default estimate was too coarse for this small heap. The full-allocation profile attributed roughly 0.44 MiB to PDF-library initialization (encoding, standard fonts, CCITT tables, and font widths). Profiling every allocation adds overhead, and the Go heap is only part of process memory; it is not the process's total resident or committed memory. The profile came from an empty profile, without user MCP servers or real task data.

The live user's existing development process measured approximately 193.6 MiB of private resident memory across the process tree: 81.8 MiB GPU, 45.6 MiB renderer, 33.3 MiB browser manager, 19.4 MiB Phoenix, and 13.5 MiB other helpers. The running development binary used `-gcflags=all=-l`; it is not directly comparable to the isolated production builds.

## Findings and limits

- GPU-disabled Phoenix used 87.6–95.9 MiB across three isolated samples, versus 148.7–177.2 MiB with GPU enabled. The corresponding GPU process fell from 62.4–72.3 MiB to 9.5–9.8 MiB. This is a memory tradeoff, not proof that software rendering is faster or better for battery life.
- The Go process held 14.0–14.7 MiB of private resident memory in five of six Phoenix samples. Optimizing its startup alone cannot remove the bulk of WebView2 memory.
- No user data, real sites, MCP workloads, scrolling, video, long-running sessions, GPU memory, energy use, or responsiveness were measured. No claim about leaks or sustained performance can be made from these short runs.
- The next comparison should exercise the same real workload and measure CPU, rendering latency, GPU memory, battery use, and memory over time before choosing a default. Profile PDF imports separately before restructuring them.
