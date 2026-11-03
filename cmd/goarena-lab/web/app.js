// goarena Benchmark Lab - single-page UI. No libraries; hand-written canvas charts.
"use strict";

(function () {

// ------------------------------------------------------------------ constants
const C = {
  green: "#55A868", greenDark: "#3B7D4C", greenLight: "#8FD19E", blue: "#4C72B0", blueLight: "#7F9CCF",
  purple: "#8172B2", sand: "#CCB974", cyan: "#64B5CD", red: "#C44E52", grey: "#8C8C8C",
  ink: "#1f2933", muted: "#6b7785", grid: "#e4e8ee",
};
const EXTRA_IMPLS = {
  "goarena-gogc400": { label: "goarena GOGC=400", color: C.cyan },
  "goarena-budget": { label: "goarena budget", color: C.grey },
};
const VERSION_COLORS = { "v0.1": "#c9d3e0", "v0.2": "#9fb1c8", "v0.3": "#6b87ad", "v0.4": "#3b5f8f", "demo": C.green };
const MiB = 1048576;
const DASH = { solid: [], dashed: [6, 4], dotted: [2, 3] };
const SWEEP_DEFAULTS = {
  goroutines: "1, 2, 4, 8, 12, 16", n: "1000000, 2000000, 5000000, 10000000",
  value_size: "64, 256, 1024, 4096", read_ratio: "50, 75, 90, 99", garbage_rate: "0, 0.25, 0.5, 1",
};
const SWEEP_FIELD = { goroutines: "goroutines", n: "n", value_size: "value_size", read_ratio: "reads_pct", garbage_rate: "garbage_rate" };

// ------------------------------------------------------------------ state
const S = {
  info: null, presets: [], impls: {}, implOrder: [],
  live: null,           // current session as rebuilt from the event stream
  logShown: 0,
  connected: false,
  page: "overview",
  xMode: "measure", latLog: true,
  history: null,        // {sessions, current}
  resultsId: null, results: null, resultsCache: {},
  baseline: null, cdfMode: "cdf", tlMetric: "ops", resSweepMetric: "ops",
  historical: null, cmp: null,
  reportId: null, reportCharts: [],
  charts: {},
};

// ------------------------------------------------------------------ helpers
const $ = (id) => document.getElementById(id);
function h(tag, attrs, ...kids) {
  const e = document.createElement(tag);
  if (attrs) {
    for (const [k, v] of Object.entries(attrs)) {
      if (v == null || v === false) continue;
      if (k === "class") e.className = v;
      else if (k === "text") e.textContent = v;
      else if (k === "style") e.style.cssText = v;
      else if (k.startsWith("on") && typeof v === "function") e.addEventListener(k.slice(2), v);
      else e.setAttribute(k, v === true ? "" : String(v));
    }
  }
  for (const kid of kids.flat(Infinity)) {
    if (kid == null || kid === false) continue;
    e.append(kid instanceof Node ? kid : document.createTextNode(String(kid)));
  }
  return e;
}
function clear(e) { while (e.firstChild) e.removeChild(e.firstChild); return e; }
function setText(id, s) { const e = $(id); if (e) e.textContent = s; }

// ------------------------------------------------------------------ i18n
// Two languages: uk (default) and en. Static HTML elements carry
// data-i18n (textContent), data-i18n-html (innerHTML) or data-i18n-title
// (title) keys; their English text is the HTML itself and is copied into
// I18N.en at start-up (captureStatic). Dynamic strings use t(key, params)
// with {name} placeholders. Implementation names (gomap, sync.Map, gobytes,
// goarena, ...) are never translated. The language comes from ?lang=uk|en
// (in the query or in the hash, e.g. #/results?lang=en), then from
// localStorage, then defaults to uk.
const LANGS = ["uk", "en"];
const LANG_KEY = "goarena-lab.lang";
const I18N = {
  en: {
    "u.k": "k", "u.M": "M", "u.G": "G", "u.B": " B", "u.KiB": " KiB", "u.MiB": " MiB", "u.GiB": " GiB",
    "u.ns": " ns", "u.us": " µs", "u.ms": " ms", "u.s": " s", "u.sAxis": "s", "u.min": "m ", "u.sec": "s",
    "u.opsps": " ops/s", "u.mopsps": " Mops/s", "u.perS": " /s", "u.msps": " ms/s", "u.Mshort": " M",
    "unit.B": "B", "unit.MiB": "MiB", "unit.s": "s", "unit.pct": "%",
    "doc.title": "goarena Benchmark Lab · {page}",
    "conn.live": "live", "conn.connected": "connected", "conn.reconnecting": "reconnecting…",
    "busy.sweep": "sweep running", "busy.session": "session running",
    "state.pending": "pending", "state.running": "running", "state.done": "done", "state.failed": "failed",
    "state.cancelled": "cancelled", "state.skipped": "skipped",
    "phase.populate": "populate", "phase.warmup": "warmup", "phase.measure": "measure",
    "bench.unknown": "unknown", "bench.building": "building", "bench.ready": "ready", "bench.missing": "missing", "bench.error": "error",
    "kind.run": "run", "kind.sweep": "sweep",
    "profile.steady": "steady", "profile.ramp": "ramp", "profile.spike": "spike", "profile.sweep-goroutines": "sweep-goroutines",
    "profile.opt.steady": "steady", "profile.opt.ramp": "ramp (goroutines step up)", "profile.opt.spike": "spike (write / garbage bursts)",
    "profile.opt.sweep-goroutines": "goroutine sweep (in-process)",
    "dist.uniform": "uniform", "dist.zipf": "Zipf (hot keys)",
    "f.impls": "Implementations",
    "f.n": "Entries N", "f.n.sub": "{v} entries",
    "f.value_size": "Value size", "f.value_size.sub": "{v} per value",
    "f.reads_pct": "Reads", "f.reads_pct.sub": "{v} % writes",
    "f.goroutines": "Goroutines", "f.goroutines.sub": "worker goroutines",
    "f.dist": "Key distribution",
    "f.zipf_s": "Zipf s", "f.zipf_s.sub": "skew, > 1",
    "f.garbage_rate": "Garbage rate", "f.garbage_rate.sub": "{v} % of ops allocate", "f.garbage_rate.sub0": "no request garbage",
    "f.garbage_bytes": "Garbage bytes", "f.garbage_bytes.sub": "per request object",
    "f.memlimit_mib": "Memory limit", "f.memlimit_mib.sub": "GOMEMLIMIT for every run", "f.memlimit_mib.sub0": "0 = no limit",
    "f.maxmem_mib": "Max memory (evict)", "f.maxmem_mib.sub": "goarena-evict budget",
    "f.duration_s": "Duration", "f.duration_s.sub": "measured phase",
    "f.warmup_s": "Warm-up", "f.warmup_s.sub": "not measured",
    "f.profile": "Load profile", "f.fill_on_miss": "Fill on miss",
    "impl.unsupported": "this goarena-bench binary does not know goarena-evict (rebuild it)",
    "est.invalid": "invalid settings",
    "est.budget": "Safe budget {b}{ram} = min(75 % of RAM, RAM − 6 GiB)", "est.ofRam": " of {r} RAM",
    "est.badValues": "values must be numbers (k/M suffixes allowed)",
    "toast.saved": " (saved to history)",
    "toast.preset": "Preset “{name}” loaded - review the settings and press Start",
    "toast.started": "Session {id} started: {n} run(s)", "toast.sweepStarted": "Sweep {id} started: {n} runs",
    "toast.cancelling": "Cancelling - the running process is killed", "toast.nothing": "Nothing is running",
    "toast.sweepNumbers": "Sweep values must be numbers", "toast.cannotLoad": "Cannot load session: {e}",
    "toast.deleted": "Deleted {id}", "toast.rescanned": "Rescanned the result directories",
    "toast.rebuilding": "Rebuilding goarena-bench…", "toast.unknownPreset": "Unknown preset {id}",
    "confirm.delete": "Delete session {id} from the history? The JSON file is removed.",
    "err.server": "Cannot reach the lab server: {e}", "err.memguard": "memory guard: ",
    "st.none": "No session yet", "st.noneHint": "choose a preset or adjust the settings and press Start",
    "st.runs": "{d} / {n} runs", "st.now": " · now: {run} ({i}/{n})", "st.elapsed": "elapsed {t}",
    "st.openResults": "open results", "st.runSession": "(the current session is a run session - see the Run page)",
    "ph.in": "{state} in {t}", "ph.starting": "starting", "ph.populate": "populate {a} / {b}",
    "ph.warmup": "warm-up {a} / {b} s", "ph.measure": "measure {a} / {b} s",
    "kpi.ops": "Throughput", "kpi.notYet": "not measured yet", "kpi.opsSub": "operations / s", "kpi.mean": " (mean)",
    "kpi.p99": "Latency p99", "kpi.gc": "GC CPU", "kpi.cycles": "{n} cycles", "kpi.heap": "Go heap", "kpi.goal": "goal {v}",
    "kpi.off": "Off-heap", "kpi.offSub": "C memory (arena + index)", "kpi.entries": "Entries", "kpi.hit": "hit ratio {v} %",
    "kpi.entSub": "{v} values · {g} goroutines", "kpi.rss": "Process RSS", "kpi.resident": "resident set",
    "strip.mops": "Mops/s ", "strip.gc": "GC CPU ",
    "dash.liveRun": "live run", "dash.noRun": "no run", "dash.idle": "idle - choose a preset and press Start", "dash.live": "live · {t}",
    "btn.settings": "Settings",
    "x.full": "seconds since process start", "x.measure": "seconds since the start of the measured phase",
    "ser.heap": "Go heap", "ser.off": "off-heap", "ser.cycles": "cycles/s", "ser.stw": "STW ms/s",
    "ent.title": "Entries", "ent.sub": "count · populated, then N", "ent.hit": "Hit ratio", "ent.hitSub": "% of reads found",
    "chart.waiting": "waiting for data", "chart.nodata": "no data", "chart.toggle": "click to hide / show",
    "chart.more": "+{n} more", "chart.value": "value", "chart.sweepEmpty": "start a sweep to see metric vs parameter",
    "sw.hintRead": "read % values, comma-separated", "sw.hintGarbage": "rates 0..1, comma-separated", "sw.hint": "comma-separated",
    "param.goroutines": "goroutines", "param.n": "entries N", "param.value_size": "value size", "param.read_ratio": "read ratio",
    "param.garbage_rate": "garbage rate",
    "pl.reads": "{v} % reads", "pl.garbage": "garbage {v}",
    "kx.value": "value", "kx.reads": "reads", "kx.garbage": "garbage",
    "sm.ops": "Throughput", "sm.p99": "p99", "sm.p999": "p99.9", "sm.gc": "GC CPU", "sm.rss": "RSS",
    "knee.none.running": "no drop detected so far",
    "knee.none.done": "no drop detected: no step lost more than 15 % throughput, doubled p99.9 or added 10 pp GC CPU",
    "knee.noSweep": "no sweep selected",
    "knee.tag.throughput-drop": "drop", "knee.tag.scaling-loss": "scaling", "knee.tag.tail-rise": "tail", "knee.tag.gc-rise": "GC",
    "knee.span": "from {a} to {b}",
    "knee.throughput-drop": "{impl}: throughput {pct} {span} ({a} -> {b} Mops/s)",
    "knee.scaling-loss": "{impl}: scaling stalls {span} - throughput {pct} for {f}x {param} (step efficiency {e}, previous {pe})",
    "knee.tail-rise": "{impl}: p99.9 x{f} {span} ({a} -> {b})",
    "knee.gc-rise": "{impl}: GC CPU +{d} pp {span} ({a} % -> {b} %)",
    "title.custom": "Custom run", "title.sweep": "Sweep: {p}",
    "opt.current": "Current: {title} ({state})", "opt.runs": "{d}/{n} runs",
    "res.none": "none", "res.session": "Session",
    "th.impl": "Implementation",
    "tm.ops": "Mops/s", "tm.p50": "p50 µs", "tm.p99": "p99 µs", "tm.p999": "p99.9 µs", "tm.max": "max ms", "tm.gcc": "GC cycles",
    "tm.gccpu": "GC CPU %", "tm.heap": "Go heap MiB", "tm.rss": "RSS MiB", "tm.off": "Off-heap MiB", "tm.pop": "Populate s",
    "tm.fgc": "Full GC ms", "tm.hit": "Hit ratio", "tm.evict": "Evictions",
    "chip.kind": "kind", "chip.state": "state", "chip.started": "started", "chip.swept": "swept", "chip.value": "value",
    "chip.reads": "reads", "chip.goroutines": "goroutines", "chip.keys": "keys", "chip.garbage": "garbage", "chip.sweptV": "swept",
    "chip.maxmem": "max memory", "chip.fill": "fill on miss", "chip.yes": "yes", "chip.duration": "duration",
    "chip.durV": "{d} s + {w} s warm-up", "chip.profile": "profile", "chip.note": "note",
    "cgo.on": "on", "cgo.off": "off (pure-Go fallback)", "cgo.unknown": "unknown",
    "cdf.x": "latency, µs (log)",
    "tl.emptySweep": "see the sweep chart above", "tl.empty": "no timeline (harness without -progress)",
    "stages.title": "Stages of the load profile ",
    "stg.stage": "Stage", "stg.start": "start s", "stg.seconds": "seconds", "stg.reads": "reads", "stg.garbage": "garbage",
    "stg.max": "max", "stg.gc": "GC CPU %",
    "hist.dir": "stored in {d}", "hist.none": "No stored sessions yet.", "hist.open": "Open", "hist.report": "Report", "hist.delete": "Delete",
    "hist.started": "Started", "hist.title": "Title", "hist.kind": "Kind", "hist.impls": "Impl.", "hist.runs": "Runs",
    "hist.state": "State", "hist.size": "Size",
    "cm.ops": "Throughput, Mops/s", "cm.p99": "Latency p99, µs", "cm.p999": "Latency p99.9, µs", "cm.gc": "GC CPU, %",
    "cm.rss": "Process RSS, MiB", "cm.heap": "Go heap, MiB", "cm.pop": "Populate, s", "cm.fgc": "Full GC, ms",
    "cmp.sources": "Sources: ", "cmp.files": "{n} files", "cmp.unreadable": ", {n} unreadable", "cmp.missing": "missing",
    "cmp.reads": "{r} % reads", "cmp.garbage": ", garbage {v}", "cmp.none": "No historical runs match this scenario.",
    "cmp.goVersion": "Go version", "cmp.cellTitle": "{n} run(s), experiment {e}", "cmp.sub": "{n} run(s) · {go}",
    "rep.none": "No session to report yet.", "rep.cannot": "Cannot load: {e}",
    "rep.h1": "goarena benchmark report - {title}", "rep.started": " · started {d}", "rep.finished": " · finished {d}", "rep.state": " · state {s}",
    "rep.cfg": "Configuration and machine", "rep.kind": "Kind", "rep.impls": "Implementations", "rep.runs": "Runs",
    "rep.runsV": "{d} of {n} produced a result", "rep.n": "Entries N", "rep.value": "Value size", "rep.reads": "Reads",
    "rep.goroutines": "Goroutines", "rep.keys": "Keys", "rep.garbage": "Request garbage", "rep.none2": "none",
    "rep.maxmem": "Max memory (evict)", "rep.fill": "Fill on miss", "rep.yes": "yes", "rep.profile": "Load profile",
    "rep.duration": "Duration", "rep.durV": "{d} s measured + {w} s warm-up",
    "rep.cpus": "Logical CPUs", "rep.unknown": "unknown", "rep.os": "OS", "rep.host": "Host", "rep.goHarness": "Go (harness)",
    "rep.cgoOn": "enabled (off-heap memory from C)", "rep.cgoOff": "disabled - pure-Go fallback", "rep.lab": "Lab",
    "rep.labV": "goarena Benchmark Lab, {v}",
    "rep.vs": "Metrics versus {p}", "rep.p999log": "Latency p99.9, µs (log)", "rep.drops": "Where it drops",
    "rep.ts": "Time series of the measured phase", "rep.cmp": "Comparison", "rep.factors": " (factors relative to {b})",
    "rep.lat": "Latency distribution", "rep.cdf": "CDF", "rep.tail": "Tail (CCDF), log", "rep.stages": "Stages ({p})",
    "rep.failed": "Failed runs",
    "rep.foot": "Generated by goarena Benchmark Lab on {d}. Each implementation ran as a separate goarena-bench process; latencies are sampled (every 16th operation, TSC clock). History file: results/lab/{id}.json",
    "ov.cores": "Logical cores", "ov.ram": "RAM", "ov.free": " ({v} free at start)", "ov.unknown": "unknown", "ov.os": "OS",
    "ov.host": "Host", "ov.labBuilt": "Lab built with", "ov.toolchain": "Toolchain", "ov.cgoEnv": "CGO_ENABLED (env)",
    "ov.budget": "Memory budget", "ov.budgetV": "{v} per run (memory guard)", "ov.results": "Results", "ov.history": "History",
    "ov.binary": "Binary", "ov.state": "State", "ov.error": "Error",
    "ov.cgoOn": "enabled - off-heap memory from C",
    "ov.cgoOff": "disabled - goarena runs its pure-Go fallback; results are not representative",
    "ov.builtWith": "Built with", "ov.module": "Module", "ov.modified": "Modified", "ov.managed": "Managed",
    "ov.managedLab": "built by the lab (bin/)", "ov.managedFlag": "given with -bench", "ov.progress": "Live progress",
    "ov.profiles": "Load profiles", "ov.fill": "Fill on miss", "ov.yes": "yes", "ov.no": "no",
    "ov.rebuild": "Rebuild harness", "ov.reprobe": "Re-probe harness",
  },
  uk: {
    "u.k": " тис.", "u.M": " млн", "u.G": " млрд", "u.B": " Б", "u.KiB": " КіБ", "u.MiB": " МіБ", "u.GiB": " ГіБ",
    "u.ns": " нс", "u.us": " мкс", "u.ms": " мс", "u.s": " с", "u.sAxis": " с", "u.min": " хв ", "u.sec": " с",
    "u.opsps": " оп./с", "u.mopsps": " млн оп./с", "u.perS": " /с", "u.msps": " мс/с", "u.Mshort": " млн",
    "unit.B": "Б", "unit.MiB": "МіБ", "unit.s": "с", "unit.pct": "%",
    "doc.title": "Бенчмарк-лабораторія goarena · {page}",
    // static HTML (data-i18n*)
    "brand.small": "Бенчмарк-лабораторія",
    "nav.overview": "Огляд", "nav.run": "Запуск", "nav.sweep": "Перебір", "nav.results": "Результати", "nav.report": "Звіт",
    "conn.connecting": "з'єднання…",
    "ov.h1": "Бенчмарк-лабораторія goarena",
    "ov.lead": "Запускає стенд <code>goarena-bench</code> на цій машині — окремий процес ОС для кожної реалізації, — " +
      "транслює його посекундні метрики на живі графіки, знаходить точки, де реалізація «просідає» під час перебору " +
      "параметра, і порівнює результати з вимірюваннями попередніх версій goarena.",
    "ov.goRun": "Запустити сценарій", "ov.goSweep": "Перебрати параметр", "ov.goResults": "Результати та історія",
    "ov.goReport": "Звіт для друку", "ov.machine": "Машина", "ov.harness": "Тестовий стенд", "ov.docs": "Документи",
    "ov.pages": "Сторінки",
    "ov.pagesList": "<li><a href=\"#/run\">Запуск</a> — пресети, параметри, живі графіки сесії</li>" +
      "<li><a href=\"#/sweep\">Перебір</a> — один параметр на кількох значеннях, пошук зламів</li>" +
      "<li><a href=\"#/results\">Результати</a> — таблиця порівняння, CDF затримки, історія, порівняння версій</li>" +
      "<li><a href=\"#/report\">Звіт</a> — зведення сесії для друку (збереження в PDF)</li>",
    "ov.impls": "Реалізації",
    "help.h2": "Як читати графіки", "help.liveH": "Живі графіки (Запуск)",
    "help.live": "<li>Вісь x — це <b>секунди від початку фази вимірювання</b> кожного запуску, тож реалізації, які " +
      "виконуються одна за одною, накладаються. Перемкніть на <b>Увесь запуск</b>, щоб побачити також фази заповнення та розігріву.</li>" +
      "<li><b>Пропускна здатність</b> — мільйони операцій за секунду в кожному 1-секундному інтервалі.</li>" +
      "<li><b>Затримка</b> — p99 (суцільна) і p99.9 (штрихова) вибіркових операцій у кожному інтервалі, у мікросекундах; " +
      "використовуйте логарифмічну шкалу, коли в однієї з реалізацій значно важчий хвіст.</li>" +
      "<li><b>CPU на GC</b> — частка доступного CPU, яку витрачає збирач сміття.</li>" +
      "<li><b>Пам'ять</b> — суцільна: RSS процесу, штрихова: купа Go, точками: пам'ять поза купою (C), МіБ. " +
      "goarena тримає дані поза купою, тож її купа Go лишається майже нульовою.</li>" +
      "<li>Вертикальні лінії позначають етапи профілю навантаження (наростання / сплески) або фази в повному поданні.</li>" +
      "<li>Наведіть курсор на графік, щоб побачити значення всіх серій у цей момент; клацніть елемент легенди, щоб приховати серію.</li>",
    "help.sweepH": "Перебори, результати та історія",
    "help.sweep": "<li>Перебір будує метрику залежно від параметра, що перебирається. <span class=\"knee-ring\"></span> Червоні " +
      "кільця позначають <b>злами</b>: пропускна здатність падає більш ніж на 15 %, p99.9 зростає більш ніж удвічі, CPU на GC " +
      "зростає більш ніж на 10 відсоткових пунктів або (для горутин) ефективність масштабування кроку падає нижче половини " +
      "ефективності попереднього кроку.</li>" +
      "<li>Таблиця порівняння виділяє найкраще значення кожного стовпця <b>жирним зеленим</b>; маленький коефіцієнт під " +
      "кожним значенням — це відношення до вибраної базової реалізації.</li>" +
      "<li>CDF затримки показує, яка частка операцій завершилася за певний час; CCDF (подання хвоста) показує частку, що " +
      "тривала довше, у логарифмічній шкалі — що нижча крива, то тонший хвіст.</li>" +
      "<li>Захист пам'яті прогнозує піковий RSS кожного запуску до його старту й відхиляє запуски понад " +
      "min(75 % RAM, RAM − 6 ГіБ).</li>",
    "run.presets": "Пресети сценаріїв <small>заповнюють форму; пресети перебору відкривають сторінку «Перебір»</small>",
    "run.settings": "Параметри", "run.guard": "Захист пам'яті <small>прогнозований піковий RSS</small>",
    "run.start": "Почати сесію", "btn.cancel": "Скасувати", "btn.toggleTitle": "показати / приховати параметри",
    "run.xMeasure": "Фаза вимірювання", "run.xFull": "Увесь запуск", "run.latLin": "Лінійна затримка",
    "run.latLog": "Логарифм. затримка", "run.hint": "наведіть курсор для значень · клік по легенді приховує серію",
    "ch.ops": "Пропускна здатність <small>оп./с</small>",
    "ch.lat": "Затримка операцій <small>мкс · p50 точками, p99 суцільна, p99.9 штрихова</small>",
    "ch.gc": "CPU на GC <small>% доступного CPU</small>",
    "ch.mem": "Пам'ять <small>МіБ · RSS суцільна, купа Go штрихова, поза купою точками</small>",
    "ent.btnEntries": "Записи", "ent.btnHit": "Частка влучань",
    "ch.cyc": "Активність GC <small>циклів/с суцільна · STW мс/с штрихова</small>",
    "run.foot": "Дані з <code>goarena-bench -progress</code> (по рядку на кожен інтервал кожного запуску), що транслюються " +
      "через <code>/api/events</code>; реалізації виконуються одна за одною, кожна у власному процесі, і накладаються за " +
      "часом від початку їхньої фази вимірювання. Самодостатня сторінка без зовнішніх ресурсів.",
    "run.log": "Журнал <small>вивід стенду (stderr)</small>",
    "sw.presets": "Пресети перебору", "sw.param": "Параметр перебору", "sw.paramLabel": "Параметр", "sw.values": "Значення",
    "sw.base": "Базові параметри", "sw.guard": "Захист пам'яті <small>найбільший запуск на реалізацію</small>",
    "sw.start": "Почати перебір", "sw.knees": "Де відбувається спад <small>злами між сусідніми кроками</small>",
    "sch.ops": "Пропускна здатність <small>млн оп./с</small>", "sch.p99": "Затримка p99 <small>мкс</small>",
    "sch.p999": "Затримка p99.9 <small>мкс, лог.</small>", "sch.gc": "CPU на GC <small>%</small>",
    "sch.rss": "RSS процесу <small>МіБ</small>", "sw.log": "Журнал",
    "res.session": "Сесія", "res.baseline": "База порівняння", "btn.exportJson": "Експорт JSON", "btn.exportCsv": "Експорт CSV",
    "btn.delete": "Видалити",
    "res.empty": "Сесій ще немає — запустіть одну на сторінці <a href=\"#/run\">Запуск</a> або <a href=\"#/sweep\">Перебір</a>.",
    "res.cmp": "Порівняння <small>найкраще значення стовпця — жирним зеленим · коефіцієнт = відношення до бази</small>",
    "res.sweep": "Перебір", "res.drops": "Де відбувається спад", "res.cdf": "Розподіл затримки",
    "res.cdfSub": "CDF фази вимірювання", "res.ccdf": "Хвіст (CCDF)",
    "res.tl": "Пропускна здатність у часі <small>фаза вимірювання, млн оп./с</small>",
    "res.history": "Історія",
    "res.versions": "Порівняння версій <small>історичні результати goarena v0.1 / v0.2 / v0.3 / v0.4 / demo, повтори усереднено</small>",
    "res.scenario": "Сценарій", "res.metric": "Метрика", "res.rescan": "Пересканувати результати",
    "rep.print": "Друк / зберегти як PDF",
    // dynamic
    "conn.live": "наживо", "conn.connected": "підключено", "conn.reconnecting": "перепідключення…",
    "busy.sweep": "триває перебір", "busy.session": "триває сесія",
    "state.pending": "очікує", "state.running": "виконується", "state.done": "готово", "state.failed": "помилка",
    "state.cancelled": "скасовано", "state.skipped": "пропущено",
    "phase.populate": "заповнення", "phase.warmup": "розігрів", "phase.measure": "вимірювання",
    "stage.base": "база", "stage.write-spike": "сплеск записів", "stage.garbage-burst": "сплеск сміття",
    "bench.unknown": "невідомо", "bench.building": "збирається", "bench.ready": "готовий", "bench.missing": "відсутній",
    "bench.error": "помилка",
    "kind.run": "запуск", "kind.sweep": "перебір",
    "profile.steady": "рівномірне", "profile.ramp": "наростання", "profile.spike": "сплески",
    "profile.sweep-goroutines": "перебір горутин",
    "profile.opt.steady": "рівномірне", "profile.opt.ramp": "наростання (горутини додаються кроками)",
    "profile.opt.spike": "сплески (записи / сміття)", "profile.opt.sweep-goroutines": "перебір горутин (в одному процесі)",
    "dist.uniform": "рівномірний", "dist.zipf": "Zipf (гарячі ключі)",
    "f.impls": "Реалізації",
    "f.n": "Кількість записів N", "f.n.sub": "записів: {v}",
    "f.value_size": "Розмір значення", "f.value_size.sub": "{v} на значення",
    "f.reads_pct": "Читання", "f.reads_pct.sub": "{v} % записів",
    "f.goroutines": "Горутини", "f.goroutines.sub": "робочі горутини",
    "f.dist": "Розподіл ключів",
    "f.zipf_s": "Zipf s", "f.zipf_s.sub": "перекіс, > 1",
    "f.garbage_rate": "Частка сміття", "f.garbage_rate.sub": "{v} % операцій алокують", "f.garbage_rate.sub0": "без сміття запитів",
    "f.garbage_bytes": "Байтів сміття", "f.garbage_bytes.sub": "на об'єкт запиту",
    "f.memlimit_mib": "Ліміт пам'яті", "f.memlimit_mib.sub": "GOMEMLIMIT для кожного запуску", "f.memlimit_mib.sub0": "0 = без ліміту",
    "f.maxmem_mib": "Макс. пам'ять (evict)", "f.maxmem_mib.sub": "бюджет goarena-evict",
    "f.duration_s": "Тривалість", "f.duration_s.sub": "фаза вимірювання",
    "f.warmup_s": "Розігрів", "f.warmup_s.sub": "не вимірюється",
    "f.profile": "Профіль навантаження", "f.fill_on_miss": "Заповнення при промаху",
    "impl.unsupported": "цей бінарник goarena-bench не знає goarena-evict (перезберіть його)",
    "est.invalid": "некоректні параметри",
    "est.budget": "Безпечний бюджет {b}{ram} = min(75 % RAM, RAM − 6 ГіБ)", "est.ofRam": " з {r} RAM",
    "est.ok": "найбільший прогнозований RSS — {max} ({impl}), у межах безпечного бюджету {b}",
    "est.over": "прогнозований RSS {impl} — {max}, більше за безпечний бюджет {b} (min(75 % RAM, RAM − 6 ГіБ)); " +
      "зменште N або розмір значення чи зніміть цю реалізацію",
    "est.badValues": "значення мають бути числами (дозволено суфікси k/M)",
    "toast.saved": " (збережено в історію)",
    "toast.preset": "Пресет «{name}» завантажено — перевірте параметри й натисніть «Почати»",
    "toast.started": "Сесію {id} запущено, запусків: {n}", "toast.sweepStarted": "Перебір {id} запущено, запусків: {n}",
    "toast.cancelling": "Скасування — поточний процес буде примусово завершено", "toast.nothing": "Нічого не виконується",
    "toast.sweepNumbers": "Значення перебору мають бути числами", "toast.cannotLoad": "Не вдалося завантажити сесію: {e}",
    "toast.deleted": "Видалено {id}", "toast.rescanned": "Каталоги результатів проскановано заново",
    "toast.rebuilding": "Перезбирання goarena-bench…", "toast.unknownPreset": "Невідомий пресет {id}",
    "confirm.delete": "Видалити сесію {id} з історії? JSON-файл буде вилучено.",
    "err.server": "Не вдалося зв'язатися із сервером лабораторії: {e}", "err.memguard": "захист пам'яті: ",
    "st.none": "Сесій ще немає", "st.noneHint": "оберіть пресет або налаштуйте параметри й натисніть «Почати»",
    "st.runs": "запуски: {d} / {n}", "st.now": " · зараз: {run} ({i}/{n})", "st.elapsed": "минуло {t}",
    "st.openResults": "відкрити результати", "st.runSession": "(поточна сесія — звичайний запуск, див. сторінку «Запуск»)",
    "ph.in": "{state} за {t}", "ph.starting": "запуск", "ph.populate": "заповнення {a} / {b}",
    "ph.warmup": "розігрів {a} / {b} с", "ph.measure": "вимірювання {a} / {b} с",
    "kpi.ops": "Пропускна здатність", "kpi.notYet": "ще не вимірюється", "kpi.opsSub": "операцій / с", "kpi.mean": " (середнє)",
    "kpi.p99": "Затримка p99", "kpi.gc": "CPU на GC", "kpi.cycles": "циклів: {n}", "kpi.heap": "Купа Go", "kpi.goal": "ціль {v}",
    "kpi.off": "Поза купою", "kpi.offSub": "пам'ять C (арена + індекс)", "kpi.entries": "Записи",
    "kpi.hit": "частка влучань {v} %", "kpi.entSub": "значення {v} · горутин: {g}", "kpi.rss": "RSS процесу",
    "kpi.resident": "резидентна пам'ять",
    "strip.mops": "млн оп./с ", "strip.gc": "CPU на GC ",
    "dash.liveRun": "живий запуск", "dash.noRun": "немає запуску",
    "dash.idle": "очікування — оберіть пресет і натисніть «Почати»", "dash.live": "наживо · {t}",
    "btn.settings": "Параметри",
    "x.full": "секунди від старту процесу", "x.measure": "секунди від початку фази вимірювання",
    "ser.heap": "купа Go", "ser.off": "поза купою", "ser.cycles": "циклів/с", "ser.stw": "STW мс/с",
    "ent.title": "Записи", "ent.sub": "кількість · заповнення, потім N", "ent.hit": "Частка влучань",
    "ent.hitSub": "% знайдених читань",
    "chart.waiting": "очікування даних", "chart.nodata": "немає даних", "chart.toggle": "клацніть, щоб приховати / показати",
    "chart.more": "ще {n}", "chart.value": "значення",
    "chart.sweepEmpty": "запустіть перебір, щоб побачити метрику залежно від параметра",
    "sw.hintRead": "значення % читань через кому", "sw.hintGarbage": "частки 0..1 через кому", "sw.hint": "через кому",
    "param.goroutines": "горутини", "param.n": "кількість записів N", "param.value_size": "розмір значення",
    "param.read_ratio": "частка читань", "param.garbage_rate": "частка сміття",
    "pl.reads": "{v} % читань", "pl.garbage": "сміття {v}",
    "kx.value": "значення", "kx.reads": "читання", "kx.garbage": "сміття",
    "sm.ops": "Пропускна здатність", "sm.gc": "CPU на GC",
    "knee.none.running": "спадів поки не виявлено",
    "knee.none.done": "спадів не виявлено: жоден крок не втратив понад 15 % пропускної здатності, не подвоїв p99.9 " +
      "і не додав 10 в. п. CPU на GC",
    "knee.noSweep": "перебір не вибрано",
    "knee.tag.throughput-drop": "спад", "knee.tag.scaling-loss": "масштаб", "knee.tag.tail-rise": "хвіст", "knee.tag.gc-rise": "GC",
    "knee.span": "від {a} до {b}",
    "knee.throughput-drop": "{impl}: пропускна здатність {pct} {span} ({a} → {b} млн оп./с)",
    "knee.scaling-loss": "{impl}: масштабування зупиняється {span} — пропускна здатність {pct} при ×{f} ({param}; " +
      "ефективність кроку {e}, попередня {pe})",
    "knee.tail-rise": "{impl}: p99.9 ×{f} {span} ({a} → {b})",
    "knee.gc-rise": "{impl}: CPU на GC +{d} в. п. {span} ({a} % → {b} %)",
    "title.custom": "Довільний запуск", "title.sweep": "Перебір: {p}",
    "opt.current": "Поточна: {title} ({state})", "opt.runs": "запуски: {d}/{n}",
    "res.none": "немає",
    "th.impl": "Реалізація",
    "tm.ops": "млн оп./с", "tm.p50": "p50, мкс", "tm.p99": "p99, мкс", "tm.p999": "p99.9, мкс", "tm.max": "макс., мс",
    "tm.gcc": "Цикли GC", "tm.gccpu": "CPU на GC, %", "tm.heap": "Купа Go, МіБ", "tm.rss": "RSS, МіБ",
    "tm.off": "Поза купою, МіБ", "tm.pop": "Заповнення, с", "tm.fgc": "Повний GC, мс", "tm.hit": "Частка влучань",
    "tm.evict": "Витіснення",
    "chip.kind": "тип", "chip.state": "стан", "chip.started": "почато", "chip.swept": "перебір", "chip.value": "значення",
    "chip.reads": "читання", "chip.goroutines": "горутини", "chip.keys": "ключі", "chip.garbage": "сміття",
    "chip.sweptV": "перебирається", "chip.maxmem": "макс. пам'ять", "chip.fill": "заповнення при промаху", "chip.yes": "так",
    "chip.duration": "тривалість", "chip.durV": "{d} с + {w} с розігріву", "chip.profile": "профіль", "chip.note": "примітка",
    "cgo.on": "увімкнено", "cgo.off": "вимкнено (запасний режим на чистому Go)", "cgo.unknown": "невідомо",
    "cdf.x": "затримка, мкс (лог.)",
    "tl.emptySweep": "див. графік перебору вище", "tl.empty": "немає часового ряду (стенд без -progress)",
    "stages.title": "Етапи профілю навантаження ",
    "stg.stage": "Етап", "stg.start": "початок, с", "stg.seconds": "секунд", "stg.reads": "читання", "stg.garbage": "сміття",
    "stg.max": "макс.", "stg.gc": "CPU на GC, %",
    "hist.dir": "зберігається в {d}", "hist.none": "Збережених сесій ще немає.", "hist.open": "Відкрити", "hist.report": "Звіт",
    "hist.delete": "Видалити", "hist.started": "Почато", "hist.title": "Назва", "hist.kind": "Тип", "hist.impls": "Реаліз.",
    "hist.runs": "Запуски", "hist.state": "Стан", "hist.size": "Розмір",
    "cm.ops": "Пропускна здатність, млн оп./с", "cm.p99": "Затримка p99, мкс", "cm.p999": "Затримка p99.9, мкс",
    "cm.gc": "CPU на GC, %", "cm.rss": "RSS процесу, МіБ", "cm.heap": "Купа Go, МіБ", "cm.pop": "Заповнення, с",
    "cm.fgc": "Повний GC, мс",
    "cmp.sources": "Джерела: ", "cmp.files": "файлів: {n}", "cmp.unreadable": ", нечитабельних: {n}", "cmp.missing": "відсутній",
    "cmp.reads": "{r} % читань", "cmp.garbage": ", сміття {v}", "cmp.none": "Жоден історичний запуск не відповідає цьому сценарію.",
    "cmp.goVersion": "Версія Go", "cmp.cellTitle": "запусків: {n}, експеримент {e}", "cmp.sub": "запусків: {n} · {go}",
    "rep.none": "Ще немає сесії для звіту.", "rep.cannot": "Не вдалося завантажити: {e}",
    "rep.h1": "Звіт про бенчмарк goarena — {title}", "rep.started": " · почато {d}", "rep.finished": " · завершено {d}",
    "rep.state": " · стан: {s}",
    "rep.cfg": "Конфігурація та машина", "rep.kind": "Тип", "rep.impls": "Реалізації", "rep.runs": "Запуски",
    "rep.runsV": "{d} з {n} дали результат", "rep.n": "Кількість записів N", "rep.value": "Розмір значення", "rep.reads": "Читання",
    "rep.goroutines": "Горутини", "rep.keys": "Ключі", "rep.garbage": "Сміття запитів", "rep.none2": "немає",
    "rep.maxmem": "Макс. пам'ять (evict)", "rep.fill": "Заповнення при промаху", "rep.yes": "так",
    "rep.profile": "Профіль навантаження", "rep.duration": "Тривалість", "rep.durV": "{d} с вимірювання + {w} с розігріву",
    "rep.cpus": "Логічні CPU", "rep.unknown": "невідомо", "rep.os": "ОС", "rep.host": "Хост", "rep.goHarness": "Go (стенд)",
    "rep.cgoOn": "увімкнено (пам'ять поза купою з C)", "rep.cgoOff": "вимкнено — запасний режим на чистому Go",
    "rep.lab": "Лабораторія", "rep.labV": "Бенчмарк-лабораторія goarena, {v}",
    "rep.vs": "Метрики залежно від параметра «{p}»", "rep.p999log": "Затримка p99.9, мкс (лог.)",
    "rep.drops": "Де відбувається спад", "rep.ts": "Часові ряди фази вимірювання", "rep.cmp": "Порівняння",
    "rep.factors": " (коефіцієнти відносно {b})", "rep.lat": "Розподіл затримки", "rep.cdf": "CDF",
    "rep.tail": "Хвіст (CCDF), лог.", "rep.stages": "Етапи ({p})", "rep.failed": "Невдалі запуски",
    "rep.foot": "Згенеровано бенчмарк-лабораторією goarena {d}. Кожна реалізація працювала в окремому процесі " +
      "goarena-bench; затримки вимірюються вибірково (кожна 16-та операція, годинник TSC). Файл історії: results/lab/{id}.json",
    "ov.cores": "Логічні ядра", "ov.ram": "RAM", "ov.free": " ({v} вільно на старті)", "ov.unknown": "невідомо", "ov.os": "ОС",
    "ov.host": "Хост", "ov.labBuilt": "Лабораторію зібрано", "ov.toolchain": "Інструментарій", "ov.cgoEnv": "CGO_ENABLED (оточення)",
    "ov.budget": "Бюджет пам'яті", "ov.budgetV": "{v} на запуск (захист пам'яті)", "ov.results": "Результати",
    "ov.history": "Історія", "ov.binary": "Бінарник", "ov.state": "Стан", "ov.error": "Помилка",
    "ov.cgoOn": "увімкнено — пам'ять поза купою з C",
    "ov.cgoOff": "вимкнено — goarena працює в запасному режимі на чистому Go; результати нерепрезентативні",
    "ov.builtWith": "Зібрано", "ov.module": "Модуль", "ov.modified": "Змінено", "ov.managed": "Керування",
    "ov.managedLab": "зібрано лабораторією (bin/)", "ov.managedFlag": "задано через -bench", "ov.progress": "Живий прогрес",
    "ov.profiles": "Профілі навантаження", "ov.fill": "Заповнення при промаху", "ov.yes": "так", "ov.no": "ні",
    "ov.rebuild": "Перезібрати стенд", "ov.reprobe": "Перевірити стенд",
    // documents (by file name)
    "doc.BENCHMARK_LAB.md": "Посібник бенчмарк-лабораторії", "doc.BENCHMARKS.md": "Бенчмарки та методика",
    "doc.README.md": "README проєкту", "doc.GUIDE.md": "Посібник користувача (GC pacing, GOMEMLIMIT)",
    "doc.DESIGN.md": "Архітектура", "doc.SUMMARY.md": "Зведення результатів (v0.3)",
    // implementation descriptions (by name; the names themselves stay as they are)
    "impl.gomap": "Класичний кеш на Go: 256 шардів map[string][]byte, кожен під захистом sync.RWMutex, GOGC=100. Кожен " +
      "ключ і кожне значення — окремий об'єкт у купі, який збирач сміття позначає на кожному циклі (близько 20 мільйонів " +
      "об'єктів при N = 10 млн), тож CPU на GC, допоміжне позначання (mark assists) на шляху запиту, хвостова затримка та " +
      "RSS (приблизно вдвічі більший за живу купу) зростають із кількістю записів.",
    "impl.gomap-gogc400": "Та сама шардована мапа з debug.SetGCPercent(400) — звичний перший крок налаштування. Збирач " +
      "запускається рідше, але купа може зрости до п'ятикратного обсягу живих даних (7,2 ГіБ купи Go при N = 10 млн), і " +
      "кожен цикл однаково позначає всі записи.",
    "impl.syncmap": "sync.Map з рядковими ключами та значеннями []byte — конкурентна мапа стандартної бібліотеки. Вона " +
      "оптимізована для ключів, які записуються один раз і читаються багато разів; внутрішнє пакування (boxing) робить її " +
      "найвибагливішою до пам'яті та GC серед базових реалізацій (6,5 ГіБ RSS і 15,7 % CPU на GC при N = 10 млн).",
    "impl.gobytes": "Кеш у купі Go без вказівників у стилі bigcache: індекс map[uint64]uint32 на кожен шард плюс великі " +
      "фрагменти []byte, що зберігають записи. Збирач не сканує пам'ять без вказівників, тож роботи для GC мало, але дані " +
      "все одно враховуються в цільовому розмірі купи. Найсильніша базова реалізація на чистому Go.",
    "impl.goarena": "store.Store з цього модуля в конфігурації за замовчуванням: записи зберігаються в слябах за класами " +
      "розмірів усередині 64-МіБ фрагментів арени, а хеш-індекс із відкритою адресацією теж розміщено поза купою — уся ця " +
      "пам'ять отримується з C через cgo. Купа Go містить лише кілька МіБ службових даних, читання не блокуються й не " +
      "алокують пам'ять. Без cgo сховище переходить у запасний режим на чистому Go (арена в купі Go).",
    "impl.goarena-paced": "goarena зі store.Config.GCPacing = true. Кожні 50 мс регулятор gcpace встановлює " +
      "GOGC_eff = GOGC × (live + off-heap) / live, тож збирач запускається так само часто, як для сховища того самого " +
      "розміру в купі, але кожен цикл позначає лише невелику купу Go; GOMEMLIMIT вважається бюджетом для пам'яті Go разом " +
      "із пам'яттю поза купою. Рекомендовано для сервісів, що алокують пам'ять на кожен запит (E9/E10).",
    "impl.goarena-evict": "goarena як обмежений кеш: store.Config.MaxMemory = макс. пам'ять (МіБ) з витісненням CLOCK, тож " +
      "замість помилки ErrNoMemory сховище витісняє холодні записи, коли бюджет пам'яті поза купою вичерпано. " +
      "Використовуйте разом із заповненням при промаху (промах записує запис, як у cache-aside) і розподілом ключів Zipf, " +
      "щоб побачити, яку частку влучань дає фіксований бюджет пам'яті.",
    // presets (by id)
    "preset.baseline-10m": "Базовий 10 млн",
    "preset.baseline-10m.d": "Усі реалізації при N = 10 млн, значення по 128 байт, 90 % читань, 16 горутин, рівномірний розподіл ключів.",
    "preset.large-20m": "Великий масштаб 20 млн",
    "preset.large-20m.d": "gomap, gobytes і goarena при N = 20 млн: ціна великої купи для GC і пам'яті.",
    "preset.zipf-hot": "Гарячі ключі (Zipf)",
    "preset.zipf-hot.d": "Нерівномірний доступ: Zipf s = 1,1 на N = 5 млн ключів, 90 % читань — конкуренція за гарячі шарди.",
    "preset.write-heavy": "Інтенсивний запис 50 %",
    "preset.write-heavy.d": "Половина операцій перезаписує значення (N = 5 млн): тиск на алокацію та списки вільних блоків.",
    "preset.request-garbage": "Сервіс зі сміттям запитів",
    "preset.request-garbage.d": "Кожна операція додатково алокує короткоживучий об'єкт запиту на 1 КіБ, тож GC працює безперервно (N = 5 млн).",
    "preset.memory-budget": "Бюджет пам'яті контейнера",
    "preset.memory-budget.d": "Навантаження зі сміттям запитів під GOMEMLIMIT = 1210 МіБ (1,25 × RSS gobytes), як у контейнері.",
    "preset.bounded-evict": "Обмежений кеш із витісненням",
    "preset.bounded-evict.d": "N = 10 млн ключів Zipf із заповненням при промаху: необмежений goarena проти goarena-evict з лімітом 512 МіБ (витіснення CLOCK).",
    "preset.load-ramp": "Наростання навантаження",
    "preset.load-ramp.d": "Стенд крок за кроком збільшує кількість активних горутин протягом 16-секундної фази вимірювання (N = 5 млн).",
    "preset.spikes": "Сплески записів / сміття",
    "preset.spikes.d": "Рівномірні читання, перервані сплеском записів і сплеском сміття запитів (16 с, N = 5 млн).",
    "preset.goroutine-sweep": "Перебір горутин",
    "preset.goroutine-sweep.d": "Пропускна здатність і хвостова затримка з 1, 2, 4, 8, 12 і 16 горутинами при N = 2 млн (5 с на запуск).",
    "preset.value-size-sweep": "Перебір розміру значення",
    "preset.value-size-sweep.d": "Значення по 64, 256, 1024 і 4096 байт при N = 1 млн: ціна копіювання та втрати на класах розмірів.",
    // historical scenarios (by id)
    "scn.e1-10m": "E1 · N = 10 млн", "scn.e1-10m.d": "базова суміш на 10 млн записів (E1; E1r у v0.3)",
    "scn.e1-5m": "E1 · N = 5 млн", "scn.e1-5m.d": "базова суміш на 5 млн записів",
    "scn.e1-1m": "E1 · N = 1 млн", "scn.e1-1m.d": "базова суміш на 1 млн записів",
    "scn.e6-zipf": "E6 · гарячі ключі Zipf", "scn.e6-zipf.d": "Zipf s = 1,1, 90 % читань, N = 5 млн",
    "scn.e6-write": "E6 · інтенсивний запис", "scn.e6-write.d": "рівномірні ключі, 50 % записів, N = 5 млн",
    "scn.e9-5m": "E9 · сміття запитів, 5 млн", "scn.e9-5m.d": "об'єкт запиту 1 КіБ на операцію, N = 5 млн",
    "scn.e9-10m": "E9 · сміття запитів, 10 млн", "scn.e9-10m.d": "об'єкт запиту 1 КіБ на операцію, N = 10 млн",
    "scn.e10": "E10 · бюджет пам'яті", "scn.e10.d": "навантаження E9 при N = 5 млн з GOMEMLIMIT",
    "scn.e4-1k": "E4 · значення 1 КіБ", "scn.e4-1k.d": "розмір значення 1024 Б при N = 2 млн",
    "scn.e4-4k": "E4 · значення 4 КіБ", "scn.e4-4k.d": "розмір значення 4096 Б при N = 2 млн",
    "scn.e5-g1": "E5 · 1 горутина", "scn.e5-g1.d": "одна горутина при N = 2 млн",
    "scn.custom": "Довільний сценарій",
  },
};
// Server-side messages (validation errors, session notes) are English; in
// Ukrainian the known ones are translated by pattern, the rest stay as is.
const SERVER_UK = [
  [/^select at least one implementation$/, () => "оберіть принаймні одну реалізацію"],
  [/^unknown implementation "(.+)"$/, (m) => "невідома реалізація «" + m[1] + "»"],
  [/^implementation "(.+)" selected twice$/, (m) => "реалізацію «" + m[1] + "» вибрано двічі"],
  [/^numbers must be finite$/, () => "числа мають бути скінченними"],
  [/^N must be in \[(\d+), (\d+)\]$/, (m) => "N має бути в межах [" + m[1] + ", " + m[2] + "]"],
  [/^value size must be in \[(\d+), (\d+)\] bytes$/, (m) => "розмір значення має бути в межах [" + m[1] + ", " + m[2] + "] байт"],
  [/^read ratio must be in \[0, 1\]$/, () => "частка читань має бути в межах [0, 1]"],
  [/^goroutines must be in \[1, (\d+)\]$/, (m) => "кількість горутин має бути в межах [1, " + m[1] + "]"],
  [/^distribution must be uniform or zipf$/, () => "розподіл має бути uniform або zipf"],
  [/^zipf s must be in \(1, 5\]$/, () => "Zipf s має бути в межах (1, 5]"],
  [/^garbage rate must be in \[0, 1\]$/, () => "частка сміття має бути в межах [0, 1]"],
  [/^garbage bytes must be in \[0, (\d+)\]$/, (m) => "байтів сміття має бути в межах [0, " + m[1] + "]"],
  [/^memory limit must be in \[0, (\d+)\] MiB$/, (m) => "ліміт пам'яті має бути в межах [0, " + m[1] + "] МіБ"],
  [/^max memory must be in \[0, (\d+)\] MiB$/, (m) => "макс. пам'ять має бути в межах [0, " + m[1] + "] МіБ"],
  [/^goarena-evict needs max memory >= (\d+) MiB$/, (m) => "goarena-evict потребує макс. пам'яті ≥ " + m[1] + " МіБ"],
  [/^duration must be in \[(.+), (.+)\] s$/, (m) => "тривалість має бути в межах [" + dec(m[1]) + ", " + m[2] + "] с"],
  [/^warm-up must be in \[0, (\d+)\] s$/, (m) => "розігрів має бути в межах [0, " + m[1] + "] с"],
  [/^load profile must be one of (.+)$/, (m) => "профіль навантаження має бути одним із " + m[1]],
  [/^invalid preset id$/, () => "некоректний ідентифікатор пресета"],
  [/^unknown sweep parameter "(.+)"$/, (m) => "невідомий параметр перебору «" + m[1] + "»"],
  [/^a sweep needs 2\.\.(\d+) values$/, (m) => "перебір потребує від 2 до " + m[1] + " значень"],
  [/^sweeps use the steady load profile$/, () => "перебори використовують рівномірний профіль навантаження"],
  [/^sweep values must be finite$/, () => "значення перебору мають бути скінченними"],
  [/^(.+) values must be integers$/, (m) => "значення параметра «" + paramByLabel(m[1]) + "» мають бути цілими"],
  [/^duplicate sweep value (.+)$/, (m) => "повторюване значення перебору " + dec(m[1])],
  [/^value ([^:]+): (.+)$/, (m) => "значення " + dec(m[1]) + ": " + trServer(m[2])],
  [/^no run produced a result$/, () => "жоден запуск не дав результату"],
  [/^(\d+) of (\d+) runs failed$/, (m) => "невдалих запусків: " + m[1] + " з " + m[2]],
  [/^history not saved: (.+)$/, (m) => "історію не збережено: " + m[1]],
  [/^a session is already running \(cancel it first\)$/, () => "сесія вже виконується (спершу скасуйте її)"],
  [/^cannot rebuild the harness while a session is running$/, () => "не можна перезбирати стенд, поки виконується сесія"],
  [/^a build is already in progress$/, () => "збирання вже триває"],
  [/^the goarena-bench binary is being prepared, try again in a few seconds$/, () => "бінарник goarena-bench готується, спробуйте за кілька секунд"],
  [/^no such session$/, () => "такої сесії немає"],
  [/^the session is running$/, () => "сесія виконується"],
  [/^invalid session id$/, () => "некоректний ідентифікатор сесії"],
  [/^invalid JSON body: (.+)$/, (m) => "некоректне тіло JSON: " + m[1]],
];
let LANG = "uk";
function t(key, p) {
  let s = I18N[LANG][key];
  if (s == null) s = I18N.en[key];
  if (s == null) return key;
  if (p) s = s.replace(/\{(\w+)\}/g, (m, k) => (p[k] != null ? p[k] : m));
  return s;
}
// tx translates a key that only the Ukrainian dictionary has (preset, scenario,
// implementation texts); otherwise it returns the server's English text.
function tx(key, fallback) { const s = I18N[LANG][key]; return s != null ? s : fallback; }
// dec uses the decimal comma in Ukrainian.
function dec(s) { s = String(s); return LANG === "uk" ? s.replace(/(\d)\.(\d)/g, "$1,$2") : s; }
function trServer(msg) {
  if (!msg || LANG === "en") return msg;
  if (msg.includes("; ")) return msg.split("; ").map(trServer).join("; ");
  for (const [re, fn] of SERVER_UK) { const m = msg.match(re); if (m) return fn(m); }
  return msg;
}
function paramByLabel(label) {
  const sp = S.info && S.info.sweep_params.find((x) => x.label === label);
  return sp ? paramName(sp.name) : label;
}
// tk translates an enum value (state, phase, ...); unknown values stay as they are.
function tk(prefix, v) {
  if (!v) return v || "";
  const s = I18N[LANG][prefix + v];
  return s != null ? s : I18N.en[prefix + v] != null ? I18N.en[prefix + v] : v;
}
function stateName(s) { return tk("state.", s); }
function phaseName(p) { return tk("phase.", p); }
function stageName(s) { return tk("stage.", s); }
function kindName(k) { return tk("kind.", k); }
function profileName(p) { return tk("profile.", p); }
function runsShort(n) { return LANG === "uk" ? n + " зап." : n + " run" + (n > 1 ? "s" : ""); }
function implDesc(m) { return tx("impl." + m.name, m.description); }
function presetName(p) { return tx("preset." + p.id, p.name); }
function presetDesc(p) { return tx("preset." + p.id + ".d", p.description); }
function scnName(s) { return tx("scn." + s.id, s.name); }
function scnDesc(s) { return tx("scn." + s.id + ".d", s.description); }
// sessTitle translates the server-made title of a session (a preset name,
// "Custom run" or "Sweep: <param>").
function sessTitle(s) {
  if (!s || !s.title) return "";
  const pr = S.presets.find((p) => p.name === s.title && (!s.preset || p.id === s.preset));
  if (pr) return presetName(pr);
  if (s.title === "Custom run") return t("title.custom");
  const m = s.title.match(/^Sweep: (.+)$/);
  if (m) return t("title.sweep", { p: paramByLabel(m[1]) });
  return s.title;
}
function langFromUrl() {
  const q = location.search + "&" + (location.hash.split("?")[1] || "");
  const m = q.match(/(?:^|[?&])lang=(uk|en)(?=&|$)/);
  return m ? m[1] : null;
}
function storedLang() {
  try { const v = localStorage.getItem(LANG_KEY); return LANGS.includes(v) ? v : null; } catch (e) { return null; }
}
function storeLang(v) { try { localStorage.setItem(LANG_KEY, v); } catch (e) { /* private mode etc. */ } }
// captureStatic records the English text of the static HTML before the
// first translation.
function captureStatic() {
  const en = I18N.en;
  for (const el of document.querySelectorAll("[data-i18n]")) if (en[el.dataset.i18n] == null) en[el.dataset.i18n] = el.textContent;
  for (const el of document.querySelectorAll("[data-i18n-html]")) if (en[el.dataset.i18nHtml] == null) en[el.dataset.i18nHtml] = el.innerHTML;
  for (const el of document.querySelectorAll("[data-i18n-title]")) if (en[el.dataset.i18nTitle] == null) en[el.dataset.i18nTitle] = el.getAttribute("title") || "";
}
function applyStatic() {
  document.documentElement.lang = LANG;
  for (const el of document.querySelectorAll("[data-i18n]")) el.textContent = t(el.dataset.i18n);
  for (const el of document.querySelectorAll("[data-i18n-html]")) el.innerHTML = t(el.dataset.i18nHtml);
  for (const el of document.querySelectorAll("[data-i18n-title]")) el.title = t(el.dataset.i18nTitle);
  for (const b of document.querySelectorAll("#lang-switch button")) b.classList.toggle("on", b.dataset.v === LANG);
}
// stripLangParam removes lang= from the URL after an explicit choice, so a
// deep link does not override the switcher on the next navigation.
function stripLangParam() {
  const strip = (s) => s.replace(/([?&])lang=\w*(&|$)/, (m, a, b) => (b ? a : "")).replace(/[?&]$/, "");
  const search = strip(location.search), hash = strip(location.hash);
  if (search !== location.search || hash !== location.hash) {
    try { history.replaceState(null, "", location.pathname + search + hash); } catch (e) { /* ignore */ }
  }
}
function initLangSwitch() {
  $("lang-switch").addEventListener("click", (e) => {
    const b = e.target.closest("button");
    if (!b || !LANGS.includes(b.dataset.v)) return;
    storeLang(b.dataset.v);
    stripLangParam();
    if (b.dataset.v !== LANG) setLang(b.dataset.v);
  });
}
// setLang switches the language and re-renders everything in place: forms,
// charts and the session view keep their state, the event stream stays open.
function setLang(v) {
  LANG = v;
  applyStatic();
  if (!S.info) return; // still starting: init renders in the new language
  renderOverview();
  renderPresets();
  for (const f of [runForm, sweepForm]) if (f) { f.renderImpls(); f.updateState(); }
  markPreset();
  relabelSweepParams();
  updateSweepHint();
  for (const pg of ["run", "sweep"]) toggleLabel(pg);
  for (const id of ["run-estimate", "sweep-estimate"]) { const el = $(id); if (el._est) renderEstimate(el, el._est[0], el._est[1]); }
  updateConn();
  fillSessionSelects();
  renderRunLive();
  renderSweepLive();
  if (S.results) renderResults(S.results);
  renderHistoryTable();
  relabelScenarios();
  renderSources();
  renderComparison();
  if (S.page === "report") loadReport();
  setDocTitle();
  for (const c of Object.values(S.charts)) c.request();
}

async function api(method, url, body) {
  const opt = { method, headers: {} };
  if (method === "POST") {
    opt.headers["Content-Type"] = "application/json";
    opt.body = JSON.stringify(body === undefined ? {} : body);
  }
  const r = await fetch(url, opt);
  const txt = await r.text();
  let data = null;
  try { data = txt ? JSON.parse(txt) : null; } catch (e) { data = { error: txt }; }
  if (!r.ok) {
    const err = new Error((data && data.error) || r.statusText || ("HTTP " + r.status));
    err.status = r.status; err.data = data;
    throw err;
  }
  return data;
}

let toastTimer = 0;
function toast(msg, bad) {
  const el = $("toast");
  el.textContent = msg;
  el.className = "toast" + (bad ? " bad" : "");
  clearTimeout(toastTimer);
  toastTimer = setTimeout(() => el.classList.add("hidden"), bad ? 7000 : 3500);
}

function debounce(fn, ms) {
  let tm = 0;
  return (...a) => { clearTimeout(tm); tm = setTimeout(() => fn(...a), ms); };
}

// ------------------------------------------------------------------ formatting
// trimRaw is the machine format (input fields); trim the displayed one
// (decimal comma in Ukrainian).
function trimRaw(v, d) {
  if (v == null || !isFinite(v)) return "–";
  let s = v.toFixed(d);
  if (s.includes(".")) s = s.replace(/0+$/, "").replace(/\.$/, "");
  return s;
}
function trim(v, d) { return dec(trimRaw(v, d)); }
function fmtCount(v) {
  if (v == null || !isFinite(v)) return "–";
  const a = Math.abs(v);
  if (a >= 1e9) return trim(v / 1e9, 2) + t("u.G");
  if (a >= 1e6) return trim(v / 1e6, 2) + t("u.M");
  if (a >= 1e3) return trim(v / 1e3, 1) + t("u.k");
  return trim(v, 2);
}
function fmtBytes(b) {
  if (b == null || !isFinite(b)) return "–";
  if (b >= 1 << 30) return trim(b / (1 << 30), 2) + t("u.GiB");
  if (b >= 1 << 20) return trim(b / (1 << 20), 1) + t("u.MiB");
  if (b >= 1 << 10) return trim(b / (1 << 10), 1) + t("u.KiB");
  return Math.round(b) + t("u.B");
}
function fmtMiB(m) { return m >= 1024 ? trim(m / 1024, 1) + t("u.GiB") : Math.round(m) + t("u.MiB"); }
function fmtLat(ns) {
  if (ns == null || !isFinite(ns)) return "–";
  if (ns >= 1e6) return trim(ns / 1e6, 2) + t("u.ms");
  if (ns >= 1e3) return trim(ns / 1e3, ns >= 1e5 ? 0 : 2) + t("u.us");
  return Math.round(ns) + t("u.ns");
}
const fmtSec = (v, d) => trim(v, d == null ? 1 : d) + t("u.s");
const fmtPct = (v, d) => trim(v, d == null ? 2 : d) + " %";
function fmtUs(us) {
  if (us == null || !isFinite(us)) return "–";
  if (us >= 1000) return trim(us, 0);
  if (us >= 100) return trim(us, 1);
  return trim(us, 2);
}
function fmtAxis(v) {
  const a = Math.abs(v);
  if (a >= 1e9) return trim(v / 1e9, 1) + t("u.G");
  if (a >= 1e6) return trim(v / 1e6, 1) + t("u.M");
  if (a >= 1e4) return trim(v / 1e3, 1) + t("u.k");
  if (a >= 100) return trim(v, 0);
  if (a >= 1) return trim(v, 2);
  if (a === 0) return "0";
  return dec(String(+v.toPrecision(2)));
}
function fmtDur(s) {
  if (!isFinite(s)) return "–";
  const m = Math.floor(s / 60), r = Math.floor(s % 60);
  return m ? m + t("u.min") + String(r).padStart(2, "0") + t("u.sec") : r + t("u.sec");
}
function fmtDate(iso) {
  const d = new Date(iso);
  if (isNaN(d)) return "–";
  const p = (n) => String(n).padStart(2, "0");
  return d.getFullYear() + "-" + p(d.getMonth() + 1) + "-" + p(d.getDate()) + " " + p(d.getHours()) + ":" + p(d.getMinutes());
}
function paramLabel(param, x) {
  switch (param) {
    case "n": return "N=" + fmtCount(x);
    case "value_size": return fmtBytes(x);
    case "goroutines": return "G=" + x;
    case "read_ratio": return t("pl.reads", { v: trim(x * 100, 1) });
    case "garbage_rate": return t("pl.garbage", { v: trim(x, 3) });
  }
  return param + "=" + x;
}
// kneeX formats a sweep value in a knee description (as knees.go does).
function kneeX(param, x) {
  switch (param) {
    case "n": return "N=" + fmtCount(x);
    case "value_size": return t("kx.value") + "=" + fmtBytes(x);
    case "goroutines": return "G=" + trim(x, 0);
    case "read_ratio": return t("kx.reads") + "=" + trim(x * 100, 1) + " %";
    case "garbage_rate": return t("kx.garbage") + "=" + trim(x, 3);
  }
  return param + "=" + trim(x, 3);
}
// kneeText describes a knee in the current language (English: the server's text).
function kneeText(k) {
  if (LANG === "en" && k.message) return k.message;
  const span = t("knee.span", { a: kneeX(k.param, k.from_x), b: kneeX(k.param, k.to_x) });
  const rel = k.from_value > 0 ? (k.to_value - k.from_value) / k.from_value : 0;
  const pr = Math.round(rel * 100), pct = (pr > 0 ? "+" : "") + trim(pr, 0) + " %";
  const p = { impl: k.impl, span, pct };
  switch (k.kind) {
    case "throughput-drop":
      return t("knee.throughput-drop", Object.assign(p, { a: trim(k.from_value / 1e6, 2), b: trim(k.to_value / 1e6, 2) }));
    case "scaling-loss": {
      const pm = (k.message || "").match(/previous (-?[\d.]+)\)/);
      return t("knee.scaling-loss", Object.assign(p, { f: trim(k.to_x / k.from_x, 2), param: paramName(k.param),
        e: trim(k.change, 2), pe: pm ? dec(pm[1]) : "–" }));
    }
    case "tail-rise":
      return t("knee.tail-rise", Object.assign(p, { f: trim(k.to_value / k.from_value, 1), a: fmtLat(k.from_value), b: fmtLat(k.to_value) }));
    case "gc-rise":
      return t("knee.gc-rise", Object.assign(p, { d: trim(k.to_value - k.from_value, 1), a: trim(k.from_value, 1), b: trim(k.to_value, 1) }));
  }
  return k.message || k.kind;
}
function paramShort(param, x) {
  switch (param) {
    case "n": return fmtCount(x);
    case "value_size": return fmtBytes(x);
    case "read_ratio": return trim(x * 100, 1) + "%";
    default: return trim(x, 3);
  }
}
function implMeta(name) {
  return S.impls[name] || EXTRA_IMPLS[name] || { name, label: name, color: C.grey };
}
function swatch(color) { return h("span", { class: "swatch", style: "--c:" + color }); }
function implName(name) { const m = implMeta(name); return h("span", { class: "implname" }, swatch(m.color), m.label); }

// ------------------------------------------------------------------ charts
function niceStep(span, n) {
  const raw = span / Math.max(1, n);
  const p = Math.pow(10, Math.floor(Math.log10(raw)));
  const f = raw / p;
  return (f <= 1 ? 1 : f <= 2 ? 2 : f <= 2.5 ? 2.5 : f <= 5 ? 5 : 10) * p;
}
function niceMax(v) {
  if (!(v > 0)) return 1;
  const p = Math.pow(10, Math.floor(Math.log10(v))), f = v / p;
  return (f <= 1 ? 1 : f <= 1.5 ? 1.5 : f <= 2 ? 2 : f <= 2.5 ? 2.5 : f <= 3 ? 3 : f <= 4 ? 4 : f <= 5 ? 5 : f <= 6 ? 6 : f <= 8 ? 8 : 10) * p;
}

// Chart draws line series on a canvas. series: [{key, label, color, dash, width, points:[[x,y]], dots, marks:[i], legendKey, legendDash}]
class Chart {
  constructor(canvas, opts) {
    this.cv = canvas;
    this.o = Object.assign({ yFmt: fmtAxis, xFmt: fmtAxis, tipX: null, tipY: null, logX: false, logY: false,
      xCats: null, yMin: null, yMax: null, empty: () => t("chart.waiting"), legend: null, scale: 0, left: 54, xTitle: "" }, opts || {});
    this.series = []; this.markers = []; this.hover = null; this.hidden = new Set(); this.raf = 0;
    const wrap = canvas.parentElement;
    wrap.classList.add("chart-wrap");
    this.tip = h("div", { class: "chart-tip" });
    canvas.after(this.tip);
    this.onMove = (e) => { const r = canvas.getBoundingClientRect(); this.hover = { x: e.clientX - r.left, y: e.clientY - r.top }; this.request(); };
    this.onLeave = () => { this.hover = null; this.tip.style.display = "none"; this.request(); };
    canvas.addEventListener("mousemove", this.onMove);
    canvas.addEventListener("mouseleave", this.onLeave);
    if (window.ResizeObserver) { this.ro = new ResizeObserver(() => this.request()); this.ro.observe(canvas); }
  }
  destroy() { if (this.ro) this.ro.disconnect(); this.tip.remove(); }
  set(series, markers, extra) {
    this.series = series || []; this.markers = markers || [];
    if (extra) Object.assign(this.o, extra);
    this.renderLegend();
    this.request();
  }
  request() {
    if (this.raf) return;
    this.raf = requestAnimationFrame(() => { this.raf = 0; this.draw(); });
  }
  renderLegend() {
    const el = this.o.legend;
    if (!el) return;
    clear(el);
    const seen = new Set();
    for (const s of this.series) {
      const k = s.legendKey || s.key || s.label;
      if (!s.label || seen.has(k)) continue;
      seen.add(k);
      const it = h("span", { class: "item" + (this.hidden.has(k) ? " off" : ""), title: t("chart.toggle") },
        h("i", { class: "line " + (s.legendDash || (s.dash && s.dash.length ? (s.dash[0] <= 2 ? "dotted" : "dashed") : "")), style: "--c:" + s.color }),
        s.legendLabel || s.label);
      it.addEventListener("click", () => {
        if (this.hidden.has(k)) this.hidden.delete(k); else this.hidden.add(k);
        it.classList.toggle("off");
        this.request();
      });
      el.append(it);
    }
    if (this.o.legendNote) el.append(h("span", { class: "key" }, this.o.legendNote));
  }
  visible() { return this.series.filter((s) => !this.hidden.has(s.legendKey || s.key || s.label)); }
  draw() {
    const cv = this.cv, o = this.o;
    const w = cv.clientWidth, hgt = cv.clientHeight;
    if (!w || !hgt) return;
    const dpr = o.scale || window.devicePixelRatio || 1;
    if (cv.width !== Math.round(w * dpr) || cv.height !== Math.round(hgt * dpr)) {
      cv.width = Math.round(w * dpr); cv.height = Math.round(hgt * dpr);
    }
    const g = cv.getContext("2d");
    g.setTransform(dpr, 0, 0, dpr, 0, 0);
    g.clearRect(0, 0, w, hgt);
    g.font = "11px Segoe UI, system-ui, sans-serif";
    const L = o.left, R = 14, T = 12, B = o.xTitle ? 34 : 22;
    const pw = w - L - R, ph = hgt - T - B;
    const series = this.visible();
    const okX = (x) => isFinite(x) && (!o.logX || x > 0);
    const okY = (y) => isFinite(y) && (!o.logY || y > 0);
    let x0 = Infinity, x1 = -Infinity, y0 = Infinity, y1 = -Infinity;
    for (const s of series) for (const [x, y] of s.points) {
      if (!okX(x) || !okY(y)) continue;
      if (x < x0) x0 = x; if (x > x1) x1 = x; if (y < y0) y0 = y; if (y > y1) y1 = y;
    }
    g.strokeStyle = C.grid; g.lineWidth = 1;
    if (!isFinite(x0)) {
      g.strokeRect(L + 0.5, T + 0.5, pw, ph);
      g.fillStyle = C.muted; g.textAlign = "center"; g.textBaseline = "middle";
      g.fillText(typeof o.empty === "function" ? o.empty() : o.empty, L + pw / 2, T + ph / 2);
      this.tip.style.display = "none";
      return;
    }
    // x domain
    if (o.xCats) { x0 = -0.5; x1 = o.xCats.length - 0.5; }
    else if (o.xMin != null) { x0 = Math.min(x0, o.xMin); }
    if (o.xMax != null) x1 = Math.max(x1, o.xMax);
    if (x1 === x0) { if (o.logX) { x0 /= 2; x1 *= 2; } else { x0 -= 1; x1 += 1; } }
    // y domain
    if (o.logY) {
      y0 = Math.pow(10, Math.floor(Math.log10(y0)));
      y1 = Math.pow(10, Math.ceil(Math.log10(y1)));
      if (y1 <= y0) y1 = y0 * 10;
      if (o.yMax != null) y1 = Math.min(y1, o.yMax);
    } else {
      y0 = o.yMin != null ? Math.min(o.yMin, y0) : Math.min(0, y0);
      y1 = o.yMax != null ? o.yMax : niceMax(y1 * 1.06);
      if (y1 <= y0) y1 = y0 + 1;
    }
    const lx = (v) => o.logX ? Math.log10(v) : v, ly = (v) => o.logY ? Math.log10(v) : v;
    const X = (v) => L + (pw * (lx(v) - lx(x0))) / (lx(x1) - lx(x0));
    const Y = (v) => T + ph - (ph * (ly(Math.max(v, o.logY ? y0 : -Infinity)) - ly(y0))) / (ly(y1) - ly(y0));
    // y grid
    g.textAlign = "right"; g.textBaseline = "middle"; g.fillStyle = C.muted;
    const yt = [];
    if (o.logY) { for (let e = Math.round(Math.log10(y0)); e <= Math.round(Math.log10(y1)); e++) yt.push(Math.pow(10, e)); }
    else { const st = niceStep(y1 - y0, Math.max(2, Math.floor(ph / 46))); for (let v = Math.ceil(y0 / st) * st; v <= y1 + st * 1e-9; v += st) yt.push(v); }
    for (const v of yt) {
      const y = Math.round(Y(v)) + 0.5;
      g.strokeStyle = C.grid; g.beginPath(); g.moveTo(L, y); g.lineTo(L + pw, y); g.stroke();
      g.fillText(o.yFmt(v), L - 6, y);
    }
    // x ticks
    g.textAlign = "center"; g.textBaseline = "top";
    if (o.xCats) {
      const every = Math.max(1, Math.ceil(o.xCats.length / Math.max(1, Math.floor(pw / 56))));
      o.xCats.forEach((lab, i) => { if (i % every === 0) g.fillText(lab, X(i), T + ph + 5); });
    } else if (o.logX) {
      for (let e = Math.floor(Math.log10(x0)); e <= Math.ceil(Math.log10(x1)); e++) {
        const v = Math.pow(10, e);
        if (v < x0 * 0.999 || v > x1 * 1.001) continue;
        const x = Math.round(X(v)) + 0.5;
        g.strokeStyle = C.grid; g.beginPath(); g.moveTo(x, T); g.lineTo(x, T + ph); g.stroke();
        g.fillText(o.xFmt(v), x, T + ph + 5);
      }
    } else {
      const st = niceStep(x1 - x0, Math.max(2, Math.floor(pw / 80)));
      for (let v = Math.ceil(x0 / st) * st; v <= x1 + st * 1e-9; v += st) g.fillText(o.xFmt(v), X(v), T + ph + 5);
    }
    if (o.xTitle) { g.fillStyle = C.muted; g.fillText(o.xTitle, L + pw / 2, T + ph + 19); }
    g.strokeStyle = "#c9d1db"; g.beginPath(); g.moveTo(L + 0.5, T); g.lineTo(L + 0.5, T + ph + 0.5); g.lineTo(L + pw, T + ph + 0.5); g.stroke();
    // markers (stages / phases)
    g.save();
    g.setLineDash([3, 3]); g.lineWidth = 1; g.textAlign = "left"; g.textBaseline = "top";
    let lastLabelX = -1e9;
    for (const m of this.markers) {
      if (!(m.x >= x0 && m.x <= x1)) continue;
      const x = Math.round(X(m.x)) + 0.5;
      g.strokeStyle = m.color || "#9aa5b1"; g.beginPath(); g.moveTo(x, T); g.lineTo(x, T + ph); g.stroke();
      if (m.label && x - lastLabelX > 40) {
        g.fillStyle = m.color || C.muted;
        g.fillText(m.label, x + 3, T + 1);
        lastLabelX = x + g.measureText(m.label).width;
      }
    }
    g.restore();
    // series
    g.save();
    g.beginPath(); g.rect(L, T - 2, pw + 2, ph + 4); g.clip();
    for (const s of series) {
      const pts = s.points.filter(([x, y]) => okX(x) && okY(y));
      if (!pts.length) continue;
      g.strokeStyle = s.color; g.lineWidth = s.width || 2; g.lineJoin = "round"; g.lineCap = "round";
      g.setLineDash(s.dash || []);
      g.beginPath();
      pts.forEach(([x, y], i) => { const px = X(x), py = Y(y); if (i) g.lineTo(px, py); else g.moveTo(px, py); });
      g.stroke();
      g.setLineDash([]);
      if (s.dots || pts.length === 1) {
        g.fillStyle = s.color;
        for (const [x, y] of pts) { g.beginPath(); g.arc(X(x), Y(y), 3, 0, Math.PI * 2); g.fill(); }
      }
      if (s.marks) {
        g.strokeStyle = C.red; g.lineWidth = 2;
        for (const i of s.marks) {
          const p = s.points[i];
          if (!p || !okX(p[0]) || !okY(p[1])) continue;
          g.beginPath(); g.arc(X(p[0]), Y(p[1]), 7, 0, Math.PI * 2); g.stroke();
        }
      }
    }
    g.restore();
    // hover
    if (!this.hover || this.hover.x < L || this.hover.x > L + pw || this.hover.y < T || this.hover.y > T + ph) {
      this.tip.style.display = "none";
      return;
    }
    let best = null;
    for (const s of series) for (const [x, y] of s.points) {
      if (!okX(x) || !okY(y)) continue;
      const d = Math.abs(X(x) - this.hover.x);
      if (!best || d < best.d) best = { d, x };
    }
    const snap = o.xCats ? Math.max(40, pw / Math.max(1, o.xCats.length) / 2 + 1) : 40;
    if (!best || best.d > snap) { this.tip.style.display = "none"; return; }
    const hx = X(best.x);
    g.strokeStyle = "rgba(31,41,51,.35)"; g.lineWidth = 1; g.setLineDash([]);
    g.beginPath(); g.moveTo(Math.round(hx) + 0.5, T); g.lineTo(Math.round(hx) + 0.5, T + ph); g.stroke();
    const rows = [];
    for (const s of series) {
      let bp = null, bd = Infinity;
      for (const p of s.points) {
        if (!okX(p[0]) || !okY(p[1])) continue;
        const d = Math.abs(X(p[0]) - hx);
        if (d < bd) { bd = d; bp = p; }
      }
      if (!bp || bd > 14) continue;
      g.fillStyle = s.color; g.beginPath(); g.arc(X(bp[0]), Y(bp[1]), 3.5, 0, Math.PI * 2); g.fill();
      rows.push({ s, p: bp });
    }
    if (!rows.length) { this.tip.style.display = "none"; return; }
    const tipY = o.tipY || o.yFmt;
    const head = o.xCats ? o.xCats[Math.round(best.x)] : (o.tipX ? o.tipX(best.x) : fmtAxis(best.x));
    clear(this.tip);
    this.tip.append(h("div", { class: "hd" }, head));
    for (const r of rows.slice(0, 24)) {
      this.tip.append(h("div", { class: "r" }, h("i", { style: "--c:" + r.s.color }), h("span", null, r.s.tipLabel || r.s.label || ""),
        h("b", null, tipY(r.p[1], r.s))));
    }
    if (rows.length > 24) this.tip.append(h("div", { class: "hint" }, t("chart.more", { n: rows.length - 24 })));
    this.tip.style.display = "block";
    const tw = this.tip.offsetWidth, th = this.tip.offsetHeight;
    let left = hx + 14;
    if (left + tw > w - 4) left = hx - 14 - tw;
    let top = Math.min(Math.max(4, this.hover.y - th / 2), hgt - th - 4);
    this.tip.style.left = cv.offsetLeft + Math.max(0, left) + "px";
    this.tip.style.top = cv.offsetTop + top + "px";
  }
}

// BarChart draws grouped bars: groups = [{label, bars:[{label, value, color, err, sub}]}]
class BarChart {
  constructor(canvas, opts) {
    this.cv = canvas; this.o = Object.assign({ yFmt: fmtAxis, tipFmt: null, scale: 0, empty: () => t("chart.nodata") }, opts || {});
    this.groups = []; this.hover = null; this.raf = 0; this.rects = [];
    canvas.parentElement.classList.add("chart-wrap");
    this.tip = h("div", { class: "chart-tip" }); canvas.after(this.tip);
    canvas.addEventListener("mousemove", (e) => { const r = canvas.getBoundingClientRect(); this.hover = { x: e.clientX - r.left, y: e.clientY - r.top }; this.request(); });
    canvas.addEventListener("mouseleave", () => { this.hover = null; this.tip.style.display = "none"; this.request(); });
    if (window.ResizeObserver) { this.ro = new ResizeObserver(() => this.request()); this.ro.observe(canvas); }
  }
  destroy() { if (this.ro) this.ro.disconnect(); this.tip.remove(); }
  set(groups) { this.groups = groups || []; this.request(); }
  request() { if (!this.raf) this.raf = requestAnimationFrame(() => { this.raf = 0; this.draw(); }); }
  draw() {
    const cv = this.cv, o = this.o, w = cv.clientWidth, hgt = cv.clientHeight;
    if (!w || !hgt) return;
    const dpr = o.scale || window.devicePixelRatio || 1;
    if (cv.width !== Math.round(w * dpr) || cv.height !== Math.round(hgt * dpr)) { cv.width = Math.round(w * dpr); cv.height = Math.round(hgt * dpr); }
    const g = cv.getContext("2d");
    g.setTransform(dpr, 0, 0, dpr, 0, 0); g.clearRect(0, 0, w, hgt);
    g.font = "11px Segoe UI, system-ui, sans-serif";
    const L = 54, R = 10, T = 12, B = 26, pw = w - L - R, ph = hgt - T - B;
    let max = 0;
    for (const gr of this.groups) for (const b of gr.bars) if (isFinite(b.value)) max = Math.max(max, b.value + (b.err || 0));
    if (!this.groups.length || max <= 0) {
      g.strokeStyle = C.grid; g.strokeRect(L + 0.5, T + 0.5, pw, ph);
      g.fillStyle = C.muted; g.textAlign = "center"; g.fillText(typeof o.empty === "function" ? o.empty() : o.empty, L + pw / 2, T + ph / 2);
      return;
    }
    max = niceMax(max * 1.05);
    const Y = (v) => T + ph - (ph * v) / max;
    const st = niceStep(max, Math.max(2, Math.floor(ph / 46)));
    g.textAlign = "right"; g.textBaseline = "middle"; g.fillStyle = C.muted;
    for (let v = 0; v <= max + st * 1e-9; v += st) {
      const y = Math.round(Y(v)) + 0.5;
      g.strokeStyle = C.grid; g.beginPath(); g.moveTo(L, y); g.lineTo(L + pw, y); g.stroke();
      g.fillText(o.yFmt(v), L - 6, y);
    }
    const gw = pw / this.groups.length;
    this.rects = [];
    g.textAlign = "center"; g.textBaseline = "top";
    this.groups.forEach((gr, gi) => {
      const n = gr.bars.length, inner = gw * 0.78, bw = Math.min(30, inner / Math.max(1, n));
      const start = L + gi * gw + (gw - bw * n) / 2;
      gr.bars.forEach((b, bi) => {
        const x = start + bi * bw;
        if (isFinite(b.value) && b.value > 0) {
          const y = Y(b.value);
          g.fillStyle = b.color; g.fillRect(x + 1, y, bw - 2, T + ph - y);
          if (b.err) {
            g.strokeStyle = C.ink; g.lineWidth = 1;
            const ey0 = Y(b.value - b.err), ey1 = Y(b.value + b.err), cx = x + bw / 2;
            g.beginPath(); g.moveTo(cx, ey0); g.lineTo(cx, ey1); g.moveTo(cx - 3, ey1); g.lineTo(cx + 3, ey1); g.moveTo(cx - 3, ey0); g.lineTo(cx + 3, ey0); g.stroke();
          }
          this.rects.push({ x, y, w: bw, h: T + ph - y, b, gr });
        } else {
          g.fillStyle = "#b3bcc6"; g.fillText("–", x + bw / 2, T + ph - 14);
        }
      });
      g.fillStyle = C.ink;
      g.fillText(gr.label, L + gi * gw + gw / 2, T + ph + 6);
    });
    g.strokeStyle = "#c9d1db"; g.beginPath(); g.moveTo(L + 0.5, T + ph + 0.5); g.lineTo(L + pw, T + ph + 0.5); g.stroke();
    const hv = this.hover && this.rects.find((r) => this.hover.x >= r.x && this.hover.x <= r.x + r.w && this.hover.y >= T && this.hover.y <= T + ph);
    if (!hv) { this.tip.style.display = "none"; return; }
    g.strokeStyle = C.ink; g.lineWidth = 1.5; g.strokeRect(hv.x + 1, hv.y, hv.w - 2, hv.h);
    clear(this.tip);
    this.tip.append(h("div", { class: "hd" }, hv.gr.label + " · " + hv.b.label),
      h("div", { class: "r" }, h("i", { style: "--c:" + hv.b.color }), h("span", null, o.metricLabel || t("chart.value")),
        h("b", null, (o.tipFmt || o.yFmt)(hv.b.value))));
    if (hv.b.sub) this.tip.append(h("div", { class: "hint" }, hv.b.sub));
    this.tip.style.display = "block";
    const tw = this.tip.offsetWidth;
    let left = hv.x + hv.w + 8;
    if (left + tw > w) left = hv.x - tw - 8;
    this.tip.style.left = cv.offsetLeft + Math.max(0, left) + "px";
    this.tip.style.top = cv.offsetTop + Math.max(0, hv.y - 10) + "px";
  }
}

// ------------------------------------------------------------------ settings form
// label: i18n key f.<k>; option labels and units are i18n keys too.
const FIELDS = [
  { k: "n", type: "int", min: 1000, step: 1000000, sub: (v) => t("f.n.sub", { v: fmtCount(v) }) },
  { k: "value_size", unit: "unit.B", type: "int", min: 8, step: 64, sub: (v) => t("f.value_size.sub", { v: fmtBytes(v) }) },
  { k: "reads_pct", unit: "unit.pct", type: "num", min: 0, max: 100, step: 5, sub: (v) => t("f.reads_pct.sub", { v: trim(100 - v, 1) }) },
  { k: "goroutines", type: "int", min: 1, max: 1024, step: 1, sub: () => t("f.goroutines.sub") },
  { k: "dist", type: "select", options: [["uniform", "dist.uniform"], ["zipf", "dist.zipf"]] },
  { k: "zipf_s", type: "num", min: 1.01, max: 5, step: 0.05, sub: () => t("f.zipf_s.sub") },
  { k: "garbage_rate", type: "num", min: 0, max: 1, step: 0.25, sub: (v) => v > 0 ? t("f.garbage_rate.sub", { v: trim(v * 100, 0) }) : t("f.garbage_rate.sub0") },
  { k: "garbage_bytes", unit: "unit.B", type: "int", min: 0, step: 256, sub: () => t("f.garbage_bytes.sub") },
  { k: "memlimit_mib", unit: "unit.MiB", type: "int", min: 0, step: 128, sub: (v) => v > 0 ? t("f.memlimit_mib.sub") : t("f.memlimit_mib.sub0") },
  { k: "maxmem_mib", unit: "unit.MiB", type: "int", min: 0, step: 128, sub: () => t("f.maxmem_mib.sub") },
  { k: "duration_s", unit: "unit.s", type: "num", min: 0.2, step: 1, sub: () => t("f.duration_s.sub") },
  { k: "warmup_s", unit: "unit.s", type: "num", min: 0, step: 1, sub: () => t("f.warmup_s.sub") },
  { k: "profile", type: "select", options: [["steady", "profile.opt.steady"], ["ramp", "profile.opt.ramp"], ["spike", "profile.opt.spike"], ["sweep-goroutines", "profile.opt.sweep-goroutines"]] },
  { k: "fill_on_miss", type: "check" },
];

class SettingsForm {
  constructor(root, opts) {
    this.root = root; this.opts = opts || {}; this.inputs = {}; this.subs = {}; this.fields = {}; this.listeners = [];
    this.preset = null; this.swept = null;
    clear(root);
    this.implBox = h("div", { class: "impl-checks" });
    root.append(h("div", { class: "field wide" }, h("span", { "data-i18n": "f.impls" }, t("f.impls"))), this.implBox);
    const form = h("div", { class: "form" });
    for (const f of FIELDS) {
      if (this.opts.sweep && f.k === "profile") continue;
      let input;
      if (f.type === "select") {
        input = h("select", null, f.options.map(([v, l]) => h("option", { value: v, "data-i18n": l }, t(l))));
      } else if (f.type === "check") {
        input = h("input", { type: "checkbox" });
      } else {
        input = h("input", { type: "number", min: f.min, max: f.max, step: f.step || "any", inputmode: "decimal" });
      }
      this.inputs[f.k] = input;
      let field;
      if (f.type === "check") {
        field = h("label", { class: "field check" }, input, h("span", { "data-i18n": "f." + f.k }, t("f." + f.k)));
      } else {
        const sub = h("div", { class: "sub" });
        this.subs[f.k] = sub;
        field = h("label", { class: "field" }, h("span", null, h("span", { "data-i18n": "f." + f.k }, t("f." + f.k)),
          f.unit ? h("small", null, " (", h("span", { "data-i18n": f.unit }, t(f.unit)), ")") : null), input, sub);
      }
      this.fields[f.k] = field;
      form.append(field);
      input.addEventListener("input", () => { this.preset = null; this.changed(true); });
      input.addEventListener("change", () => { this.changed(true); });
    }
    root.append(form);
    this.renderImpls();
  }
  renderImpls(selected) {
    const sel = new Set(selected || this.selectedImpls());
    clear(this.implBox);
    const bench = (S.info && S.info.bench) || {};
    for (const name of S.implOrder) {
      const m = implMeta(name);
      const unsupported = name === "goarena-evict" && bench.state === "ready" && !bench.evict;
      const cb = h("input", { type: "checkbox", value: name });
      cb.checked = sel.has(name);
      const lab = h("label", { class: "impl-check" + (cb.checked ? " on" : "") + (unsupported ? " unsupported" : ""), style: "--c:" + m.color,
        title: unsupported ? t("impl.unsupported") : implDesc(m) }, cb, swatch(m.color), m.label);
      cb.addEventListener("change", () => { lab.classList.toggle("on", cb.checked); this.preset = null; this.changed(true); });
      this.implBox.append(lab);
    }
  }
  selectedImpls() { return [...this.implBox.querySelectorAll("input:checked")].map((i) => i.value); }
  onChange(fn) { this.listeners.push(fn); }
  changed(user) {
    this.updateState();
    for (const fn of this.listeners) fn(user);
  }
  updateState() {
    const v = (k) => this.inputs[k] ? this.inputs[k].value : "";
    const num = (k) => Number(v(k));
    for (const f of FIELDS) if (f.sub && this.subs[f.k]) this.subs[f.k].textContent = isFinite(num(f.k)) && v(f.k) !== "" ? f.sub(num(f.k)) : "";
    this.inputs.zipf_s.disabled = v("dist") !== "zipf";
    this.inputs.garbage_bytes.disabled = !(num("garbage_rate") > 0) && this.swept !== "garbage_rate";
    this.inputs.maxmem_mib.disabled = !this.selectedImpls().includes("goarena-evict");
    const bench = (S.info && S.info.bench) || {};
    const ready = bench.state === "ready";
    if (this.inputs.profile) {
      for (const opt of this.inputs.profile.options) opt.disabled = ready && !bench.profiles && opt.value !== "steady";
    }
    this.inputs.fill_on_miss.disabled = ready && !bench.fill_on_miss;
    for (const [k, field] of Object.entries(this.fields)) field.classList.toggle("swept", this.swept != null && SWEEP_FIELD[this.swept] === k);
  }
  setSwept(param) {
    this.swept = param;
    for (const [k, inp] of Object.entries(this.inputs)) if (k !== "zipf_s" && k !== "garbage_bytes" && k !== "maxmem_mib") inp.disabled = false;
    const key = SWEEP_FIELD[param];
    if (key && this.inputs[key]) this.inputs[key].disabled = true;
    this.updateState();
    if (key && this.inputs[key]) this.inputs[key].disabled = true;
  }
  set(cfg, presetId) {
    this.renderImpls(cfg.impls || []);
    const val = {
      n: cfg.n, value_size: cfg.value_size, reads_pct: +(cfg.read_ratio * 100).toFixed(4), goroutines: cfg.goroutines,
      dist: cfg.dist || "uniform", zipf_s: cfg.zipf_s || 1.1, garbage_rate: cfg.garbage_rate || 0, garbage_bytes: cfg.garbage_bytes || 1024,
      memlimit_mib: cfg.memlimit_mib || 0, maxmem_mib: cfg.maxmem_mib || 0, duration_s: cfg.duration_s, warmup_s: cfg.warmup_s,
      profile: cfg.profile || "steady",
    };
    for (const [k, v] of Object.entries(val)) if (this.inputs[k] && v != null) this.inputs[k].value = v;
    this.inputs.fill_on_miss.checked = !!cfg.fill_on_miss;
    this.preset = presetId || null;
    this.changed(false);
  }
  get() {
    const n = (k) => { const s = this.inputs[k].value; return s === "" ? NaN : Number(s); };
    const cfg = {
      impls: this.selectedImpls(), n: Math.round(n("n")), value_size: Math.round(n("value_size")), read_ratio: n("reads_pct") / 100,
      goroutines: Math.round(n("goroutines")), dist: this.inputs.dist.value, zipf_s: n("zipf_s"),
      garbage_rate: n("garbage_rate"), garbage_bytes: Math.round(n("garbage_bytes")), memlimit_mib: Math.round(n("memlimit_mib")) || 0,
      maxmem_mib: Math.round(n("maxmem_mib")) || 0, fill_on_miss: this.inputs.fill_on_miss.checked,
      duration_s: n("duration_s"), warmup_s: n("warmup_s"), profile: this.inputs.profile ? this.inputs.profile.value : "steady",
    };
    if (!(cfg.garbage_rate > 0) && !(this.swept === "garbage_rate")) cfg.garbage_bytes = cfg.garbage_bytes || 0;
    if (!cfg.impls.includes("goarena-evict")) cfg.maxmem_mib = 0;
    if (cfg.dist !== "zipf") cfg.zipf_s = 1.1;
    if (this.preset) cfg.preset = this.preset;
    return cfg;
  }
}

// ------------------------------------------------------------------ memory guard view
function renderEstimate(el, est, grouped) {
  clear(el);
  el._est = [est, grouped]; // re-rendered on a language switch
  if (!est) { el.append(h("div", { class: "hint" }, "…")); return; }
  if (!est.valid) { el.append(h("div", { class: "msg bad" }, trServer(est.error) || t("est.invalid"))); return; }
  let rows = est.runs.map((r) => ({ impl: r.impl, mib: r.estimate_mib, ok: r.ok, label: implMeta(r.impl).label }));
  if (grouped) {
    const m = new Map();
    for (const r of rows) { const p = m.get(r.impl); if (!p || r.mib > p.mib) m.set(r.impl, r); }
    rows = [...m.values()];
  }
  const scale = Math.max(est.budget_mib, est.max_mib) || 1;
  for (const r of rows) {
    el.append(h("div", { class: "row" + (r.ok ? "" : " over"), title: r.impl + ": " + fmtMiB(r.mib) },
      h("span", null, r.label), h("div", { class: "bar" }, h("i", { style: "width:" + Math.max(1.5, (100 * r.mib) / scale) + "%;--c:" + implMeta(r.impl).color })),
      h("b", null, fmtMiB(r.mib))));
  }
  el.append(h("div", { class: "hint", style: "margin-top:6px" }, t("est.budget", { b: fmtMiB(est.budget_mib),
    ram: est.total_ram_mib ? t("est.ofRam", { r: fmtMiB(est.total_ram_mib) }) : "" })));
  el.append(h("div", { class: "msg" + (est.ok ? "" : " bad") }, estimateMessage(est)));
}
// estimateMessage is the memory-guard verdict (English: the server's text).
function estimateMessage(est) {
  if (LANG === "en" || !est.runs || !est.runs.length) return est.message;
  const worst = est.runs.reduce((a, r) => (r.estimate_mib > a.estimate_mib ? r : a), est.runs[0]);
  return t(est.ok ? "est.ok" : "est.over", { max: fmtMiB(est.max_mib), impl: implMeta(worst.impl).label, b: fmtMiB(est.budget_mib) });
}

// ------------------------------------------------------------------ live session (SSE)
function connectEvents() {
  const es = new EventSource("/api/events");
  es.onopen = () => { S.connected = true; updateConn(); };
  es.onerror = () => { S.connected = false; updateConn(); };
  es.onmessage = (m) => {
    let ev;
    try { ev = JSON.parse(m.data); } catch (e) { return; }
    handleEvent(ev);
  };
}
// loadSnapshot replays the current session once without keeping a stream open
// (#/...?snap=1, used for headless screenshots and static captures).
async function loadSnapshot() {
  try {
    const txt = await (await fetch("/api/events?once=1")).text();
    for (const line of txt.split(/\r?\n/)) {
      if (!line.startsWith("data: ")) continue;
      try { handleEvent(JSON.parse(line.slice(6))); } catch (e) { /* ignore */ }
    }
    S.connected = true;
  } catch (e) { S.connected = false; }
  updateConn();
}
function updateConn() {
  $("conn-dot").className = "dot" + (S.connected ? "" : " off");
  const busy = S.live && S.live.session.state === "running";
  setText("conn-text", S.connected ? (busy ? t("conn.live") : t("conn.connected")) : t("conn.reconnecting"));
  const b = $("busy-badge");
  b.classList.toggle("hidden", !busy);
  b.className = "badge run" + (busy ? "" : " hidden");
  if (busy) b.textContent = S.live.session.kind === "sweep" ? t("busy.sweep") : t("busy.session");
  for (const id of ["run-start", "sw-start"]) $(id).disabled = !!busy;
  for (const id of ["run-cancel", "sw-cancel"]) $(id).disabled = !busy;
}

function handleEvent(ev) {
  const d = ev.data;
  const L = S.live;
  switch (ev.type) {
    case "session_started":
      S.live = { session: d, logs: [] };
      if (d.kind === "sweep") S.sweepView = null;
      for (const r of d.runs) { r.timeline = []; }
      S.logShown = -1;
      break;
    case "run_started": if (L) Object.assign(L.session.runs[d.index], { state: "running", args: d.args, started_at: d.started_at }); break;
    case "progress":
      if (L) { const r = L.session.runs[d.index]; r.timeline.push(d.p); r.last = d.p; }
      break;
    case "log":
      if (L) { L.logs.push(d); if (L.logs.length > 2000) { L.logs.splice(0, 500); S.logShown = -1; } }
      break;
    case "run_finished":
      if (L) Object.assign(L.session.runs[d.index], { state: d.state, error: d.error, seconds: d.seconds, summary: d.summary });
      break;
    case "knees": if (L) L.session.knees = d; break;
    case "session_finished":
      if (L) {
        Object.assign(L.session, { state: d.state, error: d.error, saved: d.saved, finished_at: d.finished_at, knees: d.knees || L.session.knees });
        // only announce sessions that finish while this tab is open (not replays)
        if (Date.now() - new Date(ev.time).getTime() < 15000) {
          toast(sessTitle(L.session) + ": " + stateName(d.state) + (d.error ? " - " + trServer(d.error) : "") + (d.saved ? t("toast.saved") : ""), d.state === "failed");
        }
        S.resultsCache = {};
        refreshHistory();
      }
      break;
    case "bench":
      if (S.info) { S.info.bench = d; renderOverview(); runForm && runForm.renderImpls(); sweepForm && sweepForm.renderImpls(); }
      break;
  }
  updateConn();
  scheduleLive();
}

let liveRaf = 0;
function scheduleLive() {
  if (liveRaf) return;
  liveRaf = requestAnimationFrame(() => {
    liveRaf = 0;
    if (S.page === "run") renderRunLive();
    if (S.page === "sweep") renderSweepLive();
  });
}

// ------------------------------------------------------------------ live chart data
function runLabel(sess, r) {
  const m = implMeta(r.impl);
  return sess.kind === "sweep" && r.param_value != null ? m.label + " " + paramShort(sess.sweep.param, r.param_value) : m.label;
}
function xOf(p, mode) {
  if (mode === "full") return p.t_s;
  return p.phase === "measure" ? p.phase_t_s : NaN;
}
function lineSeries(sess, runs, fn, mode, extra) {
  const out = [];
  for (const r of runs) {
    if (!r.timeline || !r.timeline.length) continue;
    const m = implMeta(r.impl);
    const pts = [];
    for (const p of r.timeline) {
      // the harness ends a phase with a sub-interval of a few ms; its rates are noise
      if (p.interval_s > 0 && p.interval_s < 0.05 && r.timeline.length > 1) continue;
      const x = xOf(p, mode); const y = fn(p); if (isFinite(x) && y != null && isFinite(y)) pts.push([x, y]); }
    const label = runLabel(sess, r);
    out.push(Object.assign({ key: "r" + r.index, label, color: m.color, dash: m.dashed ? DASH.dashed : [], points: pts }, extra || {}));
  }
  return out;
}
function timelineMarkers(runs, mode) {
  // stage (and, in the full view, phase) boundaries of the reference run
  let ref = runs.find((r) => r.state === "running" && r.timeline && r.timeline.length);
  if (!ref) ref = [...runs].reverse().find((r) => r.timeline && r.timeline.length > 1);
  if (!ref) return [];
  const out = [];
  let prevPhase = null, prevStage = null;
  for (const p of ref.timeline) {
    const x = xOf(p, mode);
    if (!isFinite(x)) { prevPhase = p.phase; continue; }
    const start = x - (p.interval_s || 1);
    if (mode === "full" && p.phase !== prevPhase) out.push({ x: Math.max(0, start), label: phaseName(p.phase), color: "#9aa5b1" });
    if (p.stage && p.stage !== prevStage) out.push({ x: Math.max(0, start), label: stageName(p.stage), color: C.red });
    prevPhase = p.phase; prevStage = p.stage || prevStage;
  }
  return out;
}
const tipX = (x) => fmtSec(x, 1);
const xAxisSec = (v) => trim(v, 1) + t("u.sAxis");

// ------------------------------------------------------------------ Run page
let runForm = null, sweepForm = null;
function initRunPage() {
  runForm = new SettingsForm($("run-form"));
  const est = debounce(async () => {
    try { renderEstimate($("run-estimate"), await api("POST", "/api/estimate", { config: runForm.get() })); }
    catch (e) { renderEstimate($("run-estimate"), { valid: false, error: e.message }); }
  }, 220);
  runForm.onChange(() => { est(); markPreset(); });
  $("run-start").addEventListener("click", startRun);
  $("run-cancel").addEventListener("click", cancelSession);
  $("sw-cancel").addEventListener("click", cancelSession);
  segmented("run-xmode", (v) => { S.xMode = v; renderRunLive(); });
  segmented("run-latscale", (v) => { S.latLog = v === "log"; renderRunLive(); });
  segmented("ent-mode", (v) => { S.entMode = v; renderRunLive(); });
  $("run-toggle").addEventListener("click", () => setSettingsCollapsed(!$("run-layout").classList.contains("collapsed")));
  const ch = S.charts;
  const xs = { tipX, xFmt: xAxisSec };
  ch.ops = new Chart($("c-ops"), Object.assign({ legend: $("l-ops"), tipY: (v) => fmtCount(v) + t("u.opsps") }, xs));
  ch.lat = new Chart($("c-lat"), Object.assign({ legend: $("l-lat"), tipY: (v) => fmtUs(v) + t("u.us"), logY: true }, xs));
  ch.gc = new Chart($("c-gc"), Object.assign({ legend: $("l-gc"), tipY: (v) => fmtPct(v) }, xs));
  ch.mem = new Chart($("c-mem"), Object.assign({ legend: $("l-mem"), tipY: (v) => fmtMiB(v) }, xs));
  ch.ent = new Chart($("c-ent"), Object.assign({ legend: $("l-ent"), tipY: (v) => fmtCount(v) }, xs));
  ch.cyc = new Chart($("c-cyc"), Object.assign({ legend: $("l-cyc"), tipY: (v, sr) => trim(v, 2) + (sr && sr.stw ? t("u.msps") : t("u.perS")) }, xs));
}
function segmented(id, fn) {
  const el = $(id);
  el.addEventListener("click", (e) => {
    const b = e.target.closest("button");
    if (!b) return;
    for (const x of el.querySelectorAll("button")) x.classList.toggle("on", x === b);
    fn(b.dataset.v);
  });
}
function renderPresets() {
  const make = (list, el) => {
    clear(el);
    for (const p of list) {
      const b = h("button", { class: "preset", "data-id": p.id, title: presetDesc(p) },
        h("b", null, presetName(p), p.experiment ? h("span", { class: "tag" }, p.experiment) : null, p.kind === "sweep" ? h("span", { class: "tag" }, kindName("sweep")) : null),
        h("span", null, presetDesc(p)));
      b.addEventListener("click", () => applyPreset(p));
      el.append(b);
    }
  };
  make(S.presets, $("preset-list"));
  make(S.presets.filter((p) => p.kind === "sweep"), $("sweep-preset-list"));
}
function applyPreset(p, quiet) {
  if (p.kind === "sweep") {
    sweepForm.set(p.config, p.id);
    $("sw-param").value = p.sweep_param;
    $("sw-values").value = p.sweep_values.map((v) => p.sweep_param === "read_ratio" ? trimRaw(v * 100, 2) : v).join(", ");
    onSweepParam(false);
    if (!quiet) location.hash = "#/sweep";
  } else {
    runForm.set(p.config, p.id);
    if (!quiet && location.hash !== "#/run") location.hash = "#/run";
  }
  markPreset();
  if (quiet) return;
  toast(t("toast.preset", { name: presetName(p) }));
}
function markPreset() {
  for (const b of document.querySelectorAll("#preset-list .preset")) b.classList.toggle("on", runForm && b.dataset.id === runForm.preset);
  for (const b of document.querySelectorAll("#sweep-preset-list .preset")) b.classList.toggle("on", sweepForm && b.dataset.id === sweepForm.preset);
}
async function startRun() {
  try {
    const r = await api("POST", "/api/run", runForm.get());
    toast(t("toast.started", { id: r.id, n: r.runs }));
    setSettingsCollapsed(true);
  } catch (e) {
    if (e.data && e.data.estimate) renderEstimate($("run-estimate"), e.data.estimate);
    toast(errText(e), true);
  }
}
async function cancelSession() {
  try { const r = await api("POST", "/api/cancel"); toast(r.cancelled ? t("toast.cancelling") : t("toast.nothing")); }
  catch (e) { toast(errText(e), true); }
}
// errText is the message of an API error in the current language.
function errText(e) {
  if (LANG !== "en" && e.data && e.data.estimate && /^memory guard: /.test(e.message)) return t("err.memguard") + estimateMessage(e.data.estimate);
  return trServer(e.message);
}

function currentRun(sess) {
  return sess.runs.find((r) => r.state === "running") || [...sess.runs].reverse().find((r) => r.summary || (r.timeline && r.timeline.length));
}
function renderStatus(elStatus, elQueue, sess) {
  clear(elStatus);
  if (!sess) {
    elStatus.append(h("span", { class: "title" }, t("st.none")), h("span", { class: "hint" }, t("st.noneHint")));
    if (elQueue) clear(elQueue);
    return;
  }
  const done = sess.runs.filter((r) => ["done", "failed", "cancelled", "skipped"].includes(r.state)).length;
  const cur = sess.runs.find((r) => r.state === "running");
  const t0 = new Date(sess.started_at).getTime();
  const t1 = sess.finished_at ? new Date(sess.finished_at).getTime() : Date.now();
  elStatus.append(...[h("span", { class: "title" }, sessTitle(sess)), h("span", { class: "state " + sess.state }, stateName(sess.state)),
    h("span", { class: "hint" }, t("st.runs", { d: done, n: sess.runs.length }) + (cur ? t("st.now", { run: runLabel(sess, cur), i: cur.index + 1, n: sess.runs.length }) : "")),
    h("span", { class: "hint", id: "elapsed" }, t("st.elapsed", { t: fmtDur((t1 - t0) / 1000) })),
    sess.error ? h("span", { class: "warn" }, trServer(sess.error)) : null,
    sess.saved ? h("a", { href: "#/results", onclick: () => { S.resultsId = sess.id; S.resultsIdPinned = true; } }, t("st.openResults")) : null].filter(Boolean));
  if (!elQueue) return;
  clear(elQueue);
  for (const r of sess.runs) {
    const m = implMeta(r.impl);
    const extra = r.summary ? trim(r.summary.ops_per_sec / 1e6, 1) + t("u.Mshort") : r.state === "running" && r.last ? phaseName(r.last.phase) : "";
    elQueue.append(h("span", { class: "qitem " + r.state, style: "--c:" + m.color, title: r.error || stateName(r.state) },
      h("i", { class: "st" }), runLabel(sess, r), extra ? h("small", null, extra) : null));
  }
}
function kpi(label, value, sub, progress) {
  return h("div", { class: "kpi" }, h("div", { class: "label" }, label), h("div", { class: "value" }, value), h("div", { class: "sub" }, sub || " "),
    progress != null ? h("div", { class: "progress" }, h("i", { style: "width:" + Math.max(0, Math.min(100, progress * 100)) + "%" })) : null);
}
function setSettingsCollapsed(on, page) {
  page = page || "run";
  const pre = page === "sweep" ? "sw" : "run";
  $(page + "-layout").classList.toggle("collapsed", on);
  document.querySelector("#page-" + page + " .presets-card").classList.toggle("hidden", on);
  toggleLabel(page);
  for (const c of Object.values(S.charts)) c.request();
}
// toggleLabel sets the text of the "Settings" toggle of the run or sweep page.
function toggleLabel(page) {
  const on = $(page + "-layout").classList.contains("collapsed");
  $((page === "sweep" ? "sw" : "run") + "-toggle").textContent = (on ? "▶ " : "◀ ") + t("btn.settings");
}
// phaseText describes where the current run is.
function phaseText(r) {
  const p = r.last;
  if (r.state !== "running") return { text: r.seconds ? t("ph.in", { state: stateName(r.state), t: fmtDur(r.seconds) }) : stateName(r.state), prog: null };
  if (!p) return { text: t("ph.starting"), prog: null };
  if (p.phase === "populate" && p.n) return { text: t("ph.populate", { a: fmtCount(p.populated), b: fmtCount(p.n) }), prog: p.populated / p.n };
  if (p.phase === "warmup") return { text: t("ph.warmup", { a: trim(p.phase_t_s, 0), b: trim(r.config.warmup_s, 0) }), prog: p.phase_t_s / (r.config.warmup_s || 1) };
  return { text: t("ph.measure", { a: trim(p.phase_t_s, 0), b: trim(r.config.duration_s, 0) }) + (p.stage ? " · " + stageName(p.stage) : ""), prog: p.phase_t_s / (r.config.duration_s || 1) };
}
// renderKpis: the seven tiles of the httpcache dashboard, for the current implementation.
function renderKpis(el, sess) {
  clear(el);
  const r = sess && currentRun(sess);
  const p = r && r.last, s = r && r.summary;
  const live = !!(r && r.state === "running" && p);
  const pick = (fromP, fromS) => live ? fromP : s ? fromS : p ? fromP : null;
  const measuring = live && p.phase === "measure";
  const ops = live ? (measuring ? p.ops_per_sec : null) : s ? s.ops_per_sec : null;
  const ph = r ? phaseText(r) : { text: "", prog: null };
  const hr = pick(p && p.hit_ratio, s && s.hit_ratio);
  const gcCycles = pick(p && p.gc_cycles_total, s && s.gc_cycles);
  const entries = p ? (p.phase === "populate" ? p.populated : p.n) : r ? r.config.n : null;
  const showHit = hr != null && (hr < 1 || (r && r.config.fill_on_miss));
  const lat = (k) => live ? (p[k] > 0 ? p[k] : null) : s ? s[k] : null;
  el.append(
    kpi(t("kpi.ops"), ops != null ? fmtCount(ops) : (live ? phaseName(p.phase) : "–"), live && !measuring ? t("kpi.notYet") : t("kpi.opsSub") + (!live && s ? t("kpi.mean") : "")),
    kpi(t("kpi.p99"), fmtLat(lat("p99_ns")), "p50 " + fmtLat(lat("p50_ns")) + " · p99.9 " + fmtLat(lat("p999_ns"))),
    kpi(t("kpi.gc"), (() => { const v = pick(p && p.gc_cpu_percent, s && s.gc_cpu_percent); return v != null ? fmtPct(v) : "–"; })(),
      (gcCycles != null ? t("kpi.cycles", { n: fmtCount(gcCycles) }) : "") + (live && p.stw_ms != null ? " · STW " + trim(p.stw_ms, 2) + t("u.ms") : "")),
    kpi(t("kpi.heap"), fmtBytes(pick(p && p.heap_bytes, s && s.heap_bytes)), t("kpi.goal", { v: fmtBytes(pick(p && p.heap_goal_bytes, s && s.heap_goal_bytes)) })),
    kpi(t("kpi.off"), fmtBytes(pick(p && p.offheap_bytes, s && s.offheap_bytes)), t("kpi.offSub")),
    kpi(t("kpi.entries"), entries != null ? fmtCount(entries) : "–", showHit ? t("kpi.hit", { v: trim(hr * 100, 2) }) : r ? t("kpi.entSub", { v: fmtBytes(r.config.value_size), g: r.config.goroutines }) : ""),
    kpi(t("kpi.rss"), fmtBytes(pick(p && p.rss_bytes, s && s.rss_bytes)), ph.text || t("kpi.resident"), ph.prog),
  );
}
// renderStrip: one chip per implementation of the session (queue + comparison).
function renderStrip(el, sess) {
  clear(el);
  if (!sess) return;
  for (const r of sess.runs) {
    const m = implMeta(r.impl), s = r.summary, p = r.last;
    const live = r.state === "running" && p && p.phase === "measure";
    const v = (k) => s ? s[k] : live ? p[k] : null;
    const ops = v("ops_per_sec"), p999 = v("p999_ns"), gc = v("gc_cpu_percent"), rss = v("rss_bytes");
    const st = r.state === "running" ? (p ? phaseName(p.phase) : t("ph.starting")) : stateName(r.state);
    el.append(h("div", { class: "schip " + r.state, style: "--c:" + m.color, title: r.error || "" },
      h("div", { class: "nm" }, swatch(m.color), runLabel(sess, r), h("span", { class: "state " + r.state }, st)),
      h("span", { class: "m" }, t("strip.mops"), h("b", null, ops != null ? trim(ops / 1e6, 2) : "–")),
      h("span", { class: "m" }, "p99.9 ", h("b", null, p999 ? fmtLat(p999) : "–")),
      h("span", { class: "m" }, t("strip.gc"), h("b", null, gc != null ? fmtPct(gc) : "–")),
      h("span", { class: "m" }, "RSS ", h("b", null, rss ? fmtBytes(rss) : "–"))));
  }
}
function renderLog(el, L) {
  if (!L) { clear(el); return; }
  if (S.logShown < 0 || S.logShown > L.logs.length) { clear(el); S.logShown = 0; }
  const frag = document.createDocumentFragment();
  for (let i = S.logShown; i < L.logs.length; i++) {
    const d = L.logs[i];
    const m = implMeta(d.impl);
    const err = /error|fail|panic|refus/i.test(d.line);
    frag.append(h("span", { class: "imp", style: "color:" + m.color }, "[" + (d.impl || "lab") + "] "), h("span", { class: err ? "err" : null }, d.line), "\n");
  }
  S.logShown = L.logs.length;
  const atBottom = el.scrollTop + el.clientHeight >= el.scrollHeight - 30;
  el.append(frag);
  while (el.childNodes.length > 1500) el.removeChild(el.firstChild);
  if (atBottom) el.scrollTop = el.scrollHeight;
}
function renderRunLive() {
  const L = S.live, sess = L && L.session;
  renderStatus($("run-status"), null, sess);
  setText("dash-session", sess ? sessTitle(sess) : t("dash.liveRun"));
  const cur = sess && currentRun(sess);
  const badge = $("dash-impl");
  badge.textContent = cur ? runLabel(sess, cur) : t("dash.noRun");
  badge.style.setProperty("--c", cur ? implMeta(cur.impl).color : C.grey);
  const running = !!(sess && sess.state === "running");
  $("dash-dot").className = "dot" + (running ? "" : " off");
  setText("dash-state", !sess ? t("dash.idle") : running ? t("dash.live", { t: new Date().toLocaleTimeString(LANG === "uk" ? "uk-UA" : undefined) }) : stateName(sess.state));
  renderKpis($("run-kpis"), sess);
  renderStrip($("run-strip"), sess);
  const runs = sess ? sess.runs : [];
  const mode = S.xMode, ch = S.charts;
  const mk = timelineMarkers(runs, mode);
  const xt = mode === "full" ? t("x.full") : t("x.measure");
  const inPhase = (p) => p.phase === "measure" || mode === "full";
  ch.ops.set(lineSeries(sess, runs, (p) => inPhase(p) ? p.ops_per_sec : null, mode), mk, { xTitle: xt });
  const lat = [];
  for (const [f, dash, nm, w] of [[(p) => p.p50_ns, "dotted", "p50", 1.5], [(p) => p.p99_ns, "solid", "p99", 2], [(p) => p.p999_ns, "dashed", "p99.9", 1.6]]) {
    for (const s of lineSeries(sess, runs, (p) => f(p) > 0 ? f(p) / 1e3 : null, mode)) {
      s.legendKey = s.key; s.dash = DASH[dash]; s.legendDash = "solid"; s.tipLabel = s.label + " " + nm; s.width = w;
      lat.push(s);
    }
  }
  ch.lat.set(lat, mk, { logY: S.latLog, xTitle: xt });
  ch.gc.set(lineSeries(sess, runs, (p) => inPhase(p) ? p.gc_cpu_percent : null, mode), mk, { xTitle: xt });
  const mem = [];
  for (const [f, dash, nm] of [[(p) => p.rss_bytes, "solid", "RSS"], [(p) => p.heap_bytes, "dashed", t("ser.heap")], [(p) => p.offheap_bytes, "dotted", t("ser.off")]]) {
    for (const s of lineSeries(sess, runs, (p) => f(p) > 0 ? f(p) / MiB : null, mode)) {
      s.legendKey = s.key; s.dash = DASH[dash]; s.legendDash = "solid"; s.tipLabel = s.label + " " + nm; s.width = dash === "solid" ? 2 : 1.6;
      mem.push(s);
    }
  }
  ch.mem.set(mem, mk, { xTitle: xt });
  const showHit = runs.some((r) => r.config && r.config.fill_on_miss) || runs.some((r) => (r.timeline || []).some((p) => p.misses > 0));
  $("ent-mode").classList.toggle("hidden", !showHit);
  const hitMode = showHit && S.entMode === "hit";
  setText("ent-title", hitMode ? t("ent.hit") : t("ent.title"));
  setText("ent-sub", hitMode ? t("ent.hitSub") : t("ent.sub"));
  if (hitMode) ch.ent.set(lineSeries(sess, runs, (p) => p.phase === "measure" ? p.hit_ratio * 100 : null, mode), mk, { xTitle: xt, tipY: (v) => fmtPct(v), yFmt: (v) => trim(v, 0) + "%" });
  else ch.ent.set(lineSeries(sess, runs, (p) => p.phase === "populate" ? p.populated : p.n, mode), mk, { xTitle: xt, tipY: (v) => fmtCount(v), yFmt: fmtAxis });
  const cyc = [];
  for (const s of lineSeries(sess, runs, (p) => inPhase(p) ? (p.gc_cycles || 0) / (p.interval_s || 1) : null, mode)) { s.tipLabel = s.label + " " + t("ser.cycles"); cyc.push(s); }
  for (const s of lineSeries(sess, runs, (p) => inPhase(p) && p.stw_ms != null ? p.stw_ms / (p.interval_s || 1) : null, mode)) {
    s.legendKey = s.key; s.dash = DASH.dashed; s.width = 1.6; s.tipLabel = s.label + " " + t("ser.stw"); s.stw = true; cyc.push(s);
  }
  ch.cyc.set(cyc, mk, { xTitle: xt });
  renderLog($("run-log"), L);
}

// ------------------------------------------------------------------ Sweep page
function initSweepPage() {
  sweepForm = new SettingsForm($("sweep-form"), { sweep: true });
  const sel = $("sw-param");
  for (const p of S.info.sweep_params) sel.append(h("option", { value: p.name }, paramName(p.name)));
  sel.value = "goroutines";
  $("sw-values").value = SWEEP_DEFAULTS.goroutines;
  sel.addEventListener("change", () => { sweepForm.preset = null; onSweepParam(true); });
  $("sw-values").addEventListener("input", () => { sweepForm.preset = null; markPreset(); sweepEstimate(); });
  sweepForm.onChange(() => { sweepEstimate(); markPreset(); });
  $("sw-start").addEventListener("click", startSweep);
  $("sw-toggle").addEventListener("click", () => setSettingsCollapsed(!$("sweep-layout").classList.contains("collapsed"), "sweep"));
  const ch = S.charts;
  const mk = (id, lg, tip, extra) => new Chart($(id), Object.assign({ legend: $(lg), tipY: tip, xCats: [], empty: () => t("chart.sweepEmpty") }, extra || {}));
  ch.sOps = mk("s-ops", "sl-ops", (v) => trim(v, 2) + t("u.mopsps"));
  ch.sP99 = mk("s-p99", "sl-p99", (v) => fmtUs(v) + t("u.us"));
  ch.sP999 = mk("s-p999", "sl-p999", (v) => fmtUs(v) + t("u.us"), { logY: true });
  ch.sGc = mk("s-gc", "sl-gc", (v) => fmtPct(v));
  ch.sRss = mk("s-rss", "sl-rss", (v) => fmtMiB(v));
}
function onSweepParam(resetValues) {
  const p = $("sw-param").value;
  if (resetValues) $("sw-values").value = SWEEP_DEFAULTS[p] || "";
  updateSweepHint();
  sweepForm.setSwept(p);
  sweepEstimate();
  markPreset();
}
function updateSweepHint() {
  const p = $("sw-param").value;
  setText("sw-values-hint", p === "read_ratio" ? t("sw.hintRead") : p === "garbage_rate" ? t("sw.hintGarbage") : t("sw.hint"));
}
function relabelSweepParams() {
  for (const o of $("sw-param").options) o.textContent = paramName(o.value);
}
function sweepConfig() {
  const param = $("sw-param").value;
  let values = $("sw-values").value.split(/[\s,;]+/).filter(Boolean).map((s) => {
    const m = s.match(/^([\d.]+)\s*([kKmMgG]?)$/);
    if (!m) return NaN;
    return Number(m[1]) * ({ k: 1e3, m: 1e6, g: 1e9 }[m[2].toLowerCase()] || 1);
  });
  if (param === "read_ratio") values = values.map((v) => +(v / 100).toFixed(6));
  return { base: Object.assign(sweepForm.get(), { profile: "steady" }), param, values };
}
const sweepEstimate = debounce(async () => {
  if (!sweepForm) return;
  const cfg = sweepConfig();
  if (cfg.values.some((v) => !isFinite(v))) { renderEstimate($("sweep-estimate"), { valid: false, error: t("est.badValues") }); return; }
  try { renderEstimate($("sweep-estimate"), await api("POST", "/api/estimate", { sweep: cfg }), true); }
  catch (e) { renderEstimate($("sweep-estimate"), { valid: false, error: e.message }); }
}, 220);
async function startSweep() {
  const cfg = sweepConfig();
  if (cfg.values.some((v) => !isFinite(v))) { toast(t("toast.sweepNumbers"), true); return; }
  try {
    const r = await api("POST", "/api/sweep", cfg);
    toast(t("toast.sweepStarted", { id: r.id, n: r.runs }));
    setSettingsCollapsed(true, "sweep");
  } catch (e) {
    if (e.data && e.data.estimate) renderEstimate($("sweep-estimate"), e.data.estimate, true);
    toast(errText(e), true);
  }
}
// label: i18n key sm.<k>
const SWEEP_METRICS = {
  ops: { get: (s) => s.ops_per_sec / 1e6, tip: (v) => trim(v, 2) + t("u.mopsps"), knees: ["throughput-drop", "scaling-loss"] },
  p99: { get: (s) => s.p99_ns / 1e3, tip: (v) => fmtUs(v) + t("u.us"), knees: [] },
  p999: { get: (s) => s.p999_ns / 1e3, tip: (v) => fmtUs(v) + t("u.us"), knees: ["tail-rise"], logY: true },
  gc: { get: (s) => s.gc_cpu_percent, tip: (v) => fmtPct(v), knees: ["gc-rise"] },
  rss: { get: (s) => s.rss_bytes / MiB, tip: (v) => fmtMiB(v), knees: [] },
};
// sweepSeries returns one series per implementation: x = index of the value.
function sweepSeries(sess, metric) {
  const vals = sess.sweep.values, M = SWEEP_METRICS[metric];
  const byImpl = new Map();
  for (const r of sess.runs) {
    if (!byImpl.has(r.impl)) byImpl.set(r.impl, []);
    if (r.summary && r.param_value != null) byImpl.get(r.impl).push([vals.indexOf(r.param_value), M.get(r.summary)]);
  }
  const out = [];
  for (const [impl, pts] of byImpl) {
    const m = implMeta(impl);
    pts.sort((a, b) => a[0] - b[0]);
    const marks = [];
    for (const k of sess.knees || []) {
      if (k.impl !== impl || !M.knees.includes(k.kind)) continue;
      const i = pts.findIndex((p) => p[0] === vals.indexOf(k.to_x));
      if (i >= 0) marks.push(i);
    }
    out.push({ key: impl, label: m.label, color: m.color, dash: m.dashed ? DASH.dashed : [], points: pts, dots: true, marks });
  }
  return out;
}
function renderKnees(el, sess) {
  clear(el);
  const ks = (sess && sess.knees) || [];
  if (!ks.length) {
    el.append(h("li", { class: "none" }, sess && sess.kind === "sweep"
      ? (sess.state === "running" ? t("knee.none.running") : t("knee.none.done"))
      : t("knee.noSweep")));
    return;
  }
  const cls = { "throughput-drop": "", "scaling-loss": "scaling", "tail-rise": "tail", "gc-rise": "gc" };
  for (const k of ks) el.append(h("li", null, h("span", { class: "k " + (cls[k.kind] || "") }, cls[k.kind] != null ? t("knee.tag." + k.kind) : k.kind), h("span", null, kneeText(k))));
}
function renderSweepMatrix(el, sess) {
  clear(el);
  if (!sess || sess.kind !== "sweep") return;
  const vals = sess.sweep.values;
  const impls = [...new Set(sess.runs.map((r) => r.impl))];
  const tbl = h("table", null, h("thead", null, h("tr", null, h("th", { class: "l" }, paramName(sess.sweep.param)), impls.map((i) => h("th", { class: "l" }, implName(i))))));
  const tb = h("tbody");
  for (const v of vals) {
    tb.append(h("tr", null, h("td", { class: "l" }, paramShort(sess.sweep.param, v)), impls.map((impl) => {
      const r = sess.runs.find((x) => x.impl === impl && x.param_value === v);
      if (!r) return h("td", { class: "l na" }, "–");
      const txt = r.summary ? trim(r.summary.ops_per_sec / 1e6, 2) + t("u.mopsps") : r.state === "running" && r.last ? phaseName(r.last.phase) + "…" : stateName(r.state);
      return h("td", { class: "l" }, h("span", { class: "state " + r.state }, txt));
    })));
  }
  tbl.append(tb);
  el.append(tbl);
}
function paramName(p) {
  if (I18N[LANG]["param." + p] != null) return t("param." + p);
  const sp = S.info && S.info.sweep_params.find((x) => x.name === p);
  return sp ? sp.label : p;
}
function renderSweepCharts(sess, charts) {
  const ok = sess && sess.kind === "sweep";
  const cats = ok ? sess.sweep.values.map((v) => paramShort(sess.sweep.param, v)) : [];
  const xt = ok ? paramName(sess.sweep.param) : "";
  for (const [metric, chart] of charts) {
    if (!chart) continue;
    chart.set(ok ? sweepSeries(sess, metric) : [], [], { xCats: cats, xTitle: xt, tipY: SWEEP_METRICS[metric].tip, logY: !!SWEEP_METRICS[metric].logY });
  }
}
// showStoredSweep displays a stored sweep (the one in ?id= or the newest)
// on the Sweep page while no sweep is running.
async function showStoredSweep(id) {
  const H = S.history || { sessions: [] };
  if (!id) { const e = H.sessions.find((x) => x.kind === "sweep"); id = e && e.id; }
  if (!id) return;
  try { S.sweepView = await fetchSession(id); } catch (e) { return; }
  if (S.page === "sweep") renderSweepLive();
}
function renderSweepLive() {
  const liveSweep = S.live && S.live.session.kind === "sweep" ? S.live.session : null;
  const sess = liveSweep || S.sweepView || (S.live && S.live.session);
  const isSweep = sess && sess.kind === "sweep";
  renderStatus($("sw-status"), null, isSweep ? sess : null);
  if (!isSweep && sess) $("sw-status").append(h("span", { class: "hint" }, t("st.runSession")));
  renderSweepMatrix($("sw-matrix"), isSweep ? sess : null);
  const ch = S.charts;
  renderSweepCharts(isSweep ? sess : null, [["ops", ch.sOps], ["p99", ch.sP99], ["p999", ch.sP999], ["gc", ch.sGc], ["rss", ch.sRss]]);
  renderKnees($("sw-knees"), isSweep ? sess : null);
  if (liveSweep) renderLog($("sw-log"), S.live); else clear($("sw-log"));
}

// ------------------------------------------------------------------ Results page
async function refreshHistory() {
  try { S.history = await api("GET", "/api/history"); } catch (e) { S.history = { sessions: [], current: null }; }
  fillSessionSelects();
  if (S.page === "results") { renderHistoryTable(); loadResults(); }
  if (S.page === "report") loadReport();
}
function sessionOptions() {
  const opts = [];
  const H = S.history || { sessions: [] };
  if (H.current) opts.push({ id: H.current.id, text: t("opt.current", { title: sessTitle(H.current), state: stateName(H.current.state) }) });
  for (const s of H.sessions) {
    if (H.current && s.id === H.current.id) continue;
    opts.push({ id: s.id, text: fmtDate(s.started_at) + " · " + sessTitle(s) + " · " + t("opt.runs", { d: s.runs_done, n: s.runs }) + (s.state !== "done" ? " · " + stateName(s.state) : "") });
  }
  return opts;
}
function fillSessionSelects() {
  const opts = sessionOptions();
  for (const [id, key] of [["res-session", "resultsId"], ["rep-session", "reportId"]]) {
    const sel = $(id);
    clear(sel);
    for (const o of opts) sel.append(h("option", { value: o.id }, o.text));
    // follow the newest session unless the user picked one explicitly
    if (!S[key + "Pinned"] || !opts.find((o) => o.id === S[key])) { S[key] = opts.length ? opts[0].id : null; S[key + "Pinned"] = false; }
    if (S[key]) sel.value = S[key];
  }
}
async function fetchSession(id) {
  const H = S.history || {};
  const live = H.current && H.current.id === id;
  if (!live && S.resultsCache[id]) return S.resultsCache[id];
  const s = await api("GET", "/api/history/" + encodeURIComponent(id));
  for (const r of s.runs) { if (!r.timeline) r.timeline = []; if (typeof r.result === "string") { try { r.result = JSON.parse(r.result); } catch (e) { r.result = null; } } }
  if (!live) S.resultsCache[id] = s;
  return s;
}
async function loadResults() {
  const id = S.resultsId;
  $("res-empty").classList.toggle("hidden", !!id);
  $("res-body").classList.toggle("hidden", !id);
  for (const b of ["res-export-json", "res-export-csv", "res-report", "res-delete"]) $(b).classList.toggle("hidden", !id);
  if (!id) return;
  try { S.results = await fetchSession(id); } catch (e) { toast(t("toast.cannotLoad", { e: trServer(e.message) }), true); return; }
  if (S.resultsId !== id) return;
  $("res-export-json").href = "/api/export/" + id + ".json";
  $("res-export-csv").href = "/api/export/" + id + ".csv";
  renderResults(S.results);
}
// column header: i18n key tm.<k>
const TABLE_METRICS = [
  { k: "ops", get: (s) => s.ops_per_sec / 1e6, better: "high", fmt: (v) => trim(v, 2) },
  { k: "p50", get: (s) => s.p50_ns / 1e3, better: "low", fmt: fmtUs },
  { k: "p99", get: (s) => s.p99_ns / 1e3, better: "low", fmt: fmtUs },
  { k: "p999", get: (s) => s.p999_ns / 1e3, better: "low", fmt: fmtUs },
  { k: "max", get: (s) => s.max_ns / 1e6, better: "low", fmt: (v) => trim(v, 2) },
  { k: "gcc", get: (s) => s.gc_cycles, better: "low", fmt: (v) => fmtCount(v) },
  { k: "gccpu", get: (s) => s.gc_cpu_percent, better: "low", fmt: (v) => trim(v, 2) },
  { k: "heap", get: (s) => s.heap_bytes / MiB, better: "low", fmt: (v) => trim(v, v < 10 ? 1 : 0) },
  { k: "rss", get: (s) => s.rss_bytes / MiB, better: "low", fmt: (v) => trim(v, 0) },
  { k: "off", get: (s) => s.offheap_bytes / MiB, better: null, fmt: (v) => trim(v, 0) },
  { k: "pop", get: (s) => s.populate_seconds, better: "low", fmt: (v) => trim(v, 2) },
  { k: "fgc", get: (s) => s.forced_gc_ms, better: "low", fmt: (v) => trim(v, 2) },
  { k: "hit", get: (s) => s.hit_ratio != null ? s.hit_ratio * 100 : NaN, better: "high", fmt: (v) => trim(v, 2) + " %", optional: true },
  { k: "evict", get: (s) => s.evictions != null ? s.evictions : NaN, better: null, fmt: (v) => fmtCount(v), optional: true },
];
function visibleMetrics(sess) {
  const sums = sess.runs.map((r) => r.summary).filter(Boolean);
  return TABLE_METRICS.filter((m) => {
    if (!m.optional) return true;
    if (m.k === "hit") return sess.config.fill_on_miss || sums.some((s) => s.hit_ratio != null && s.hit_ratio < 1);
    if (m.k === "evict") return sums.some((s) => s.evictions > 0) || sess.runs.some((r) => r.impl === "goarena-evict");
    return true;
  });
}
function comparisonTable(sess, baseline, opts) {
  opts = opts || {};
  const metrics = visibleMetrics(sess);
  const sweep = sess.kind === "sweep" && sess.sweep;
  const groupOf = (r) => sweep ? r.param_value : 0;
  const head = h("tr", null, h("th", { class: "l" }, t("th.impl")), sweep ? h("th", null, paramName(sweep.param)) : null, metrics.map((m) => h("th", null, t("tm." + m.k))));
  const tb = h("tbody");
  const runs = sess.runs.slice().sort((a, b) => sweep ? (a.param_value - b.param_value) || (a.index - b.index) : a.index - b.index);
  let prevGroup;
  for (const r of runs) {
    const s = r.summary;
    const grp = runs.filter((x) => groupOf(x) === groupOf(r) && x.summary);
    const base = baseline && grp.find((x) => x.impl === baseline);
    const tr = h("tr", { class: prevGroup !== undefined && groupOf(r) !== prevGroup ? "group-start" : null });
    prevGroup = groupOf(r);
    tr.append(h("td", { class: "l" }, implName(r.impl), r.state !== "done" ? h("span", { class: "state " + r.state, title: r.error || "", style: "margin-left:6px" }, stateName(r.state)) : null));
    if (sweep) tr.append(h("td", null, paramShort(sweep.param, r.param_value)));
    for (const m of metrics) {
      if (!s) { tr.append(h("td", { class: "na" }, "–")); continue; }
      const v = m.get(s);
      if (!isFinite(v)) { tr.append(h("td", { class: "na" }, "–")); continue; }
      let best = false;
      if (m.better && grp.length > 1) {
        const vals = grp.map((x) => m.get(x.summary)).filter(isFinite);
        const target = m.better === "high" ? Math.max(...vals) : Math.min(...vals);
        best = v === target && vals.some((x) => x !== target);
      }
      const td = h("td", { class: best ? "best" : null }, m.fmt(v));
      if (!opts.noRatio && base && base !== r && base.summary) {
        const bv = m.get(base.summary);
        if (isFinite(bv) && bv > 0 && isFinite(v)) {
          const ratio = v / bv;
          const good = m.better === "high" ? ratio > 1.02 : m.better === "low" ? ratio < 0.98 : false;
          const bad = m.better === "high" ? ratio < 0.98 : m.better === "low" ? ratio > 1.02 : false;
          td.append(h("span", { class: "ratio" + (good ? " good" : bad ? " bad" : "") }, "×" + (ratio >= 100 ? trim(ratio, 0) : ratio >= 10 ? trim(ratio, 1) : trim(ratio, 2))));
        }
      }
      tr.append(td);
    }
    tb.append(tr);
  }
  return h("table", null, h("thead", null, head), tb);
}
function configChips(sess) {
  const c = sess.config, chips = [];
  const chip = (k, v, warn) => h("span", { class: "chip" + (warn ? " warn" : "") }, h("b", null, k), v);
  chips.push(chip(t("chip.kind"), kindName(sess.kind)), chip(t("chip.state"), stateName(sess.state), sess.state === "failed"));
  chips.push(chip(t("chip.started"), fmtDate(sess.started_at)));
  if (sess.sweep) chips.push(chip(t("chip.swept"), paramName(sess.sweep.param) + " = " + sess.sweep.values.map((v) => paramShort(sess.sweep.param, v)).join(", ")));
  if (!(sess.sweep && sess.sweep.param === "n")) chips.push(chip("N", fmtCount(c.n)));
  if (!(sess.sweep && sess.sweep.param === "value_size")) chips.push(chip(t("chip.value"), fmtBytes(c.value_size)));
  if (!(sess.sweep && sess.sweep.param === "read_ratio")) chips.push(chip(t("chip.reads"), fmtPct(c.read_ratio * 100, 1)));
  if (!(sess.sweep && sess.sweep.param === "goroutines")) chips.push(chip(t("chip.goroutines"), c.goroutines));
  chips.push(chip(t("chip.keys"), c.dist === "zipf" ? "Zipf s=" + dec(c.zipf_s) : t("dist.uniform")));
  if (c.garbage_rate > 0 || (sess.sweep && sess.sweep.param === "garbage_rate")) chips.push(chip(t("chip.garbage"), (sess.sweep && sess.sweep.param === "garbage_rate" ? t("chip.sweptV") : trim(c.garbage_rate, 2)) + " × " + fmtBytes(c.garbage_bytes)));
  if (c.memlimit_mib > 0) chips.push(chip("GOMEMLIMIT", c.memlimit_mib + t("u.MiB")));
  if (c.maxmem_mib > 0) chips.push(chip(t("chip.maxmem"), c.maxmem_mib + t("u.MiB")));
  if (c.fill_on_miss) chips.push(chip(t("chip.fill"), t("chip.yes")));
  chips.push(chip(t("chip.duration"), t("chip.durV", { d: trim(c.duration_s, 1), w: trim(c.warmup_s, 1) })));
  if (c.profile && c.profile !== "steady") chips.push(chip(t("chip.profile"), profileName(c.profile)));
  if (sess.bench) chips.push(chip("cgo", sess.bench.cgo === "1" ? t("cgo.on") : sess.bench.cgo === "0" ? t("cgo.off") : t("cgo.unknown"), sess.bench.cgo === "0"));
  const gv = sess.runs.map((r) => r.summary && r.summary.go_version).find(Boolean);
  if (gv) chips.push(chip("Go", gv));
  const av = sess.runs.map((r) => r.summary && r.summary.goarena_version).find(Boolean);
  if (av) chips.push(chip("goarena", av));
  if (sess.error) chips.push(chip(t("chip.note"), trServer(sess.error), true));
  return chips;
}
function cdfSeries(sess, mode) {
  const out = [];
  for (const r of sess.runs) {
    const b = r.result && r.result.latency_buckets;
    if (!b || !b.length) continue;
    const total = b.reduce((a, x) => a + x.n, 0);
    if (!total) continue;
    let cum = 0;
    const pts = [];
    for (const x of b) {
      cum += x.n;
      const us = Math.max(x.hi, 1) / 1e3;
      if (mode === "cdf") pts.push([us, (100 * cum) / total]);
      else { const tail = (100 * (total - cum)) / total; if (tail > 0) pts.push([us, tail]); }
    }
    const m = implMeta(r.impl);
    out.push({ key: "r" + r.index, label: runLabel(sess, r), color: m.color, dash: m.dashed ? DASH.dashed : [], points: pts, width: 1.8 });
  }
  return out;
}
// cdfOpts are the chart options of the CDF / CCDF views (built per call: the axis title is translated).
function cdfOpts(mode) {
  return mode === "cdf"
    ? { logX: true, logY: false, yMax: 100, yFmt: (v) => trim(v, 0) + "%", tipY: (v) => trim(v, 3) + " % ≤ x", xTitle: t("cdf.x") }
    : { logX: true, logY: true, yMax: 100, yFmt: (v) => (v >= 1 ? trim(v, 0) : dec(String(+v.toPrecision(1)))) + "%", tipY: (v) => (v < 0.01 ? dec(v.toPrecision(2)) : trim(v, 3)) + " % > x", xTitle: t("cdf.x") };
}
function renderResults(sess) {
  setText("res-title", sessTitle(sess) + " · " + sess.id);
  const cfg = $("res-config"); clear(cfg); cfg.append(...configChips(sess));
  const impls = [...new Set(sess.runs.map((r) => r.impl))];
  const bsel = $("res-baseline"); clear(bsel);
  bsel.append(h("option", { value: "" }, t("res.none")));
  for (const i of impls) bsel.append(h("option", { value: i }, implMeta(i).label));
  if (!impls.includes(S.baseline)) S.baseline = impls.includes("gomap") ? "gomap" : impls.includes("gobytes") ? "gobytes" : impls[0];
  bsel.value = S.baseline || "";
  const tw = $("res-table"); clear(tw); tw.append(comparisonTable(sess, S.baseline));
  // sweep
  const isSweep = sess.kind === "sweep" && sess.sweep;
  $("res-sweep-card").classList.toggle("hidden", !isSweep);
  if (isSweep) {
    setText("res-sweep-sub", paramName(sess.sweep.param) + " = " + sess.sweep.values.map((v) => paramShort(sess.sweep.param, v)).join(", "));
    const seg = $("res-sweep-metric");
    if (!seg.childNodes.length) {
      for (const k of Object.keys(SWEEP_METRICS)) seg.append(h("button", { "data-v": k, "data-i18n": "sm." + k, class: k === S.resSweepMetric ? "on" : null }, t("sm." + k)));
      segmented("res-sweep-metric", (v) => { S.resSweepMetric = v; renderResults(S.results); });
    }
    renderSweepCharts(sess, [[S.resSweepMetric, S.charts.resSweep]]);
    renderKnees($("res-knees"), sess);
  }
  // CDF
  S.charts.cdf.set(cdfSeries(sess, S.cdfMode), [], Object.assign({ tipX: (x) => fmtUs(x) + t("u.us"), xFmt: (v) => fmtUs(v) }, cdfOpts(S.cdfMode)));
  // timeline
  const tl = { ops: [(p) => p.ops_per_sec / 1e6, (v) => trim(v, 2) + t("u.mopsps"), false],
    p999: [(p) => p.p999_ns > 0 ? p.p999_ns / 1e3 : null, (v) => fmtUs(v) + t("u.us"), true],
    gc: [(p) => p.gc_cpu_percent, (v) => fmtPct(v), false],
    rss: [(p) => p.rss_bytes / MiB, (v) => fmtMiB(v), false] }[S.tlMetric];
  const runs = isSweep ? [] : sess.runs;
  S.charts.tl.set(lineSeries(sess, runs, (p) => p.phase === "measure" ? tl[0](p) : null, "measure"), timelineMarkers(runs, "measure"),
    { tipX, tipY: tl[1], logY: tl[2], xTitle: t("x.measure"), empty: isSweep ? t("tl.emptySweep") : t("tl.empty") });
  // stages
  const st = $("res-stages"); clear(st);
  const withStages = sess.runs.filter((r) => r.summary && r.summary.stages && r.summary.stages.length);
  st.classList.toggle("hidden", !withStages.length);
  if (withStages.length) st.append(h("h2", null, t("stages.title"), h("small", null, profileName(sess.config.profile))), h("div", { class: "table-wrap" }, stagesTable(withStages, sess)));
}
function stagesTable(runs, sess) {
  const tb = h("tbody");
  const names = [];
  for (const r of runs) for (const s of r.summary.stages) if (!names.includes(s.name)) names.push(s.name);
  for (const name of names) {
    let first = true;
    for (const r of runs) {
      const s = r.summary.stages.find((x) => x.name === name);
      if (!s) continue;
      tb.append(h("tr", { class: first ? "group-start" : null }, h("td", { class: "l" }, first ? stageName(name) : ""), h("td", { class: "l" }, implName(r.impl)),
        h("td", null, trim(s.start_s, 1)), h("td", null, trim(s.seconds, 1)), h("td", null, s.goroutines || "–"), h("td", null, fmtPct((s.read_ratio || 0) * 100, 0)),
        h("td", null, trim(s.garbage_rate || 0, 2)), h("td", null, trim(s.ops_per_sec / 1e6, 2)), h("td", null, fmtLat(s.latency && s.latency.p99_ns)),
        h("td", null, fmtLat(s.latency && s.latency.p999_ns)), h("td", null, fmtLat(s.latency && s.latency.max_ns)), h("td", null, trim(s.gc_cpu_percent, 2))));
      first = false;
    }
  }
  return h("table", null, h("thead", null, h("tr", null, [t("stg.stage"), t("th.impl")].map((x) => h("th", { class: "l" }, x)),
    [t("stg.start"), t("stg.seconds"), "G", t("stg.reads"), t("stg.garbage"), t("tm.ops"), "p99", "p99.9", t("stg.max"), t("stg.gc")].map((x) => h("th", null, x)))), tb);
}
function renderHistoryTable() {
  const el = $("hist-table"); clear(el);
  const H = S.history || { sessions: [] };
  setText("hist-dir", H.dir ? t("hist.dir", { d: H.dir }) : "");
  if (!H.sessions.length) { el.append(h("div", { class: "hint" }, t("hist.none"))); return; }
  const tb = h("tbody");
  for (const s of H.sessions) {
    const open = () => { S.resultsId = s.id; S.resultsIdPinned = true; $("res-session").value = s.id; loadResults(); window.scrollTo({ top: 0, behavior: "smooth" }); };
    tb.append(h("tr", { class: s.id === S.resultsId ? "selected" : null },
      h("td", { class: "l" }, fmtDate(s.started_at)), h("td", { class: "l" }, h("a", { href: "#/results", onclick: (e) => { e.preventDefault(); open(); } }, sessTitle(s))),
      h("td", { class: "l" }, kindName(s.kind) + (s.sweep_param ? " (" + paramName(s.sweep_param) + ")" : s.profile && s.profile !== "steady" ? " (" + profileName(s.profile) + ")" : "")),
      h("td", { class: "l" }, s.impls.map((i) => swatch(implMeta(i).color))), h("td", null, fmtCount(s.n)),
      h("td", null, s.runs_done + "/" + s.runs), h("td", { class: "l" }, h("span", { class: "state " + s.state }, stateName(s.state))),
      h("td", null, fmtBytes(s.size_bytes)),
      h("td", { class: "l" }, h("a", { class: "btn small", href: "#/results", onclick: (e) => { e.preventDefault(); open(); } }, t("hist.open")), " ",
        h("a", { class: "btn small", href: "#/report", onclick: () => { S.reportId = s.id; S.reportIdPinned = true; } }, t("hist.report")), " ",
        h("a", { class: "btn small", href: "/api/export/" + s.id + ".csv" }, "CSV"), " ",
        h("button", { class: "btn small danger", onclick: () => deleteSession(s.id) }, t("hist.delete")))));
  }
  el.append(h("table", null, h("thead", null, h("tr", null, [t("hist.started"), t("hist.title"), t("hist.kind"), t("hist.impls"), "N", t("hist.runs"), t("hist.state"), t("hist.size"), ""].map((x, i) =>
    h("th", { class: [0, 1, 2, 3, 6, 8].includes(i) ? "l" : null }, x)))), tb));
}
async function deleteSession(id) {
  if (!confirm(t("confirm.delete", { id }))) return;
  try {
    await api("DELETE", "/api/history/" + encodeURIComponent(id));
    delete S.resultsCache[id];
    if (S.resultsId === id) S.resultsId = null;
    if (S.reportId === id) S.reportId = null;
    toast(t("toast.deleted", { id }));
    await refreshHistory();
  } catch (e) { toast(errText(e), true); }
}
function initResultsPage() {
  $("res-session").addEventListener("change", (e) => { S.resultsId = e.target.value; S.resultsIdPinned = true; loadResults(); renderHistoryTable(); });
  $("res-baseline").addEventListener("change", (e) => { S.baseline = e.target.value || null; renderResults(S.results); });
  $("res-delete").addEventListener("click", () => S.resultsId && deleteSession(S.resultsId));
  $("res-report").addEventListener("click", () => { S.reportId = S.resultsId; S.reportIdPinned = true; });
  segmented("res-cdf-mode", (v) => { S.cdfMode = v; renderResults(S.results); });
  segmented("res-tl-metric", (v) => { S.tlMetric = v; renderResults(S.results); });
  S.charts.cdf = new Chart($("res-cdf"), { legend: $("res-cdf-legend") });
  S.charts.tl = new Chart($("res-tl"), { legend: $("res-tl-legend"), xFmt: xAxisSec });
  S.charts.resSweep = new Chart($("res-sweep-chart"), { legend: $("res-sweep-legend"), xCats: [] });
  // version comparison
  S.charts.cmp = new BarChart($("cmp-bars"));
  const ms = $("cmp-metric");
  for (const k of Object.keys(CMP_METRICS)) ms.append(h("option", { value: k, "data-i18n": "cm." + k }, t("cm." + k)));
  ms.addEventListener("change", renderComparison);
  $("cmp-scenario").addEventListener("change", loadComparison);
  $("cmp-reload").addEventListener("click", async () => { await loadHistorical(true); toast(t("toast.rescanned")); });
}
// label: i18n key cm.<k>
const CMP_METRICS = {
  ops: { get: (g) => g.ops_per_sec / 1e6, err: (g) => g.ops_per_sec_std / 1e6, fmt: (v) => trim(v, 2) },
  p99: { get: (g) => g.p99_ns / 1e3, fmt: fmtUs },
  p999: { get: (g) => g.p999_ns / 1e3, fmt: fmtUs },
  gc: { get: (g) => g.gc_cpu_percent, fmt: (v) => trim(v, 2) },
  rss: { get: (g) => g.rss_bytes / MiB, fmt: (v) => trim(v, 0) },
  heap: { get: (g) => g.heap_bytes / MiB, fmt: (v) => trim(v, 0) },
  pop: { get: (g) => g.populate_seconds, fmt: (v) => trim(v, 2) },
  fgc: { get: (g) => g.forced_gc_ms, fmt: (v) => trim(v, 2) },
};
async function loadHistorical(reload) {
  try { S.historical = await api("GET", "/api/historical" + (reload ? "?reload=1" : "")); } catch (e) { toast(errText(e), true); return; }
  const sel = $("cmp-scenario"), keep = sel.value;
  clear(sel);
  for (const s of S.historical.scenarios) sel.append(h("option", { value: s.id }, scnName(s)));
  sel.value = keep || "e1-10m";
  renderSources();
  await loadComparison();
}
function relabelScenarios() {
  if (!S.historical) return;
  for (const o of $("cmp-scenario").options) { const s = S.historical.scenarios.find((x) => x.id === o.value); if (s) o.textContent = scnName(s); }
}
function renderSources() {
  const src = $("cmp-sources"); clear(src);
  if (!S.historical) return;
  src.append(h("span", null, t("cmp.sources"), S.historical.sources.map((s, i) => [i ? " · " : "", h("b", null, s.version), " ", s.dir, ": ",
    s.exists ? t("cmp.files", { n: s.files }) + (s.go_versions.length ? " (" + s.go_versions.join(", ") + ")" : "") + (s.errors ? t("cmp.unreadable", { n: s.errors }) : "") : t("cmp.missing")])));
}
async function loadComparison() {
  const id = $("cmp-scenario").value || "e1-10m";
  try { S.cmp = await api("GET", "/api/compare?scenario=" + encodeURIComponent(id)); } catch (e) { toast(errText(e), true); return; }
  $("cmp-export-csv").href = "/api/export/compare.csv?scenario=" + encodeURIComponent(id);
  $("cmp-export-json").href = "/api/export/compare.json?scenario=" + encodeURIComponent(id);
  renderComparison();
}
function renderComparison() {
  const c = S.cmp;
  if (!c) return;
  const mk = $("cmp-metric").value || "ops", M = CMP_METRICS[mk], mLabel = t("cm." + mk);
  const sc = c.scenario;
  setText("cmp-desc", scnName(sc) + ": " + scnDesc(sc) + " - N=" + fmtCount(sc.n) + ", " + sc.value_size + t("u.B") + ", G=" + sc.goroutines + ", " +
    (sc.dist === "uniform" ? t("dist.uniform") : sc.dist) + ", " + t("cmp.reads", { r: trim(sc.read_ratio * 100, 0) }) + (sc.garbage_rate ? t("cmp.garbage", { v: dec(sc.garbage_rate) }) : "") +
    (Object.keys(c.memlimits || {}).length ? ", GOMEMLIMIT " + Object.entries(c.memlimits).map(([v, m]) => v + " " + m + t("u.MiB")).join(", ") : ""));
  const versions = c.versions;
  const el = $("cmp-table"); clear(el);
  if (!c.impls.length) { el.append(h("div", { class: "hint" }, t("cmp.none"))); S.charts.cmp.set([]); return; }
  const tb = h("tbody");
  tb.append(h("tr", null, h("td", { class: "l hint" }, t("cmp.goVersion")), versions.map((v) => h("td", { class: "hint" }, c.go_versions[v] || "–"))));
  for (const impl of c.impls) {
    tb.append(h("tr", null, h("td", { class: "l" }, implName(impl)), versions.map((v) => {
      const g = c.cells[impl] && c.cells[impl][v];
      if (!g) return h("td", { class: "na" }, "-");
      const val = M.get(g);
      return h("td", { title: t("cmp.cellTitle", { n: g.runs, e: g.experiment }) }, M.fmt(val),
        M.err && g.runs > 1 ? h("span", { class: "ratio" }, "± " + M.fmt(M.err(g)) + " (" + g.runs + ")") : h("span", { class: "ratio" }, runsShort(g.runs)));
    })));
  }
  el.append(h("table", null, h("thead", null, h("tr", null, h("th", { class: "l" }, mLabel), versions.map((v) => h("th", null, v)))), tb));
  S.charts.cmp.o.yFmt = fmtAxis;
  S.charts.cmp.o.tipFmt = M.fmt;
  S.charts.cmp.o.metricLabel = mLabel;
  S.charts.cmp.set(c.impls.map((impl) => ({ label: implMeta(impl).label,
    bars: versions.map((v) => { const g = c.cells[impl] && c.cells[impl][v]; return { label: v, value: g ? M.get(g) : NaN, err: g && M.err && g.runs > 1 ? M.err(g) : 0, color: VERSION_COLORS[v] || C.grey, sub: g ? t("cmp.sub", { n: g.runs, go: (g.go_versions || []).join(", ") }) : "" }; }) })));
  const lg = $("cmp-legend"); clear(lg);
  for (const v of versions) lg.append(h("span", { class: "item" }, swatch(VERSION_COLORS[v] || C.grey), v));
}

// ------------------------------------------------------------------ Report page
async function loadReport() {
  const el = $("report");
  for (const c of S.reportCharts) c.destroy();
  S.reportCharts = [];
  clear(el);
  const tok = S.reportTok = (S.reportTok || 0) + 1; // a newer call (route, language switch) wins
  if (!S.reportId) { el.append(h("div", { class: "empty" }, t("rep.none"))); return; }
  $("rep-session").value = S.reportId;
  let sess;
  try { sess = await fetchSession(S.reportId); } catch (e) {
    if (tok === S.reportTok) el.append(h("div", { class: "empty" }, t("rep.cannot", { e: trServer(e.message) })));
    return;
  }
  if (tok !== S.reportTok) return;
  buildReport(el, sess);
}
function reportChart(parent, title, opts) {
  const cv = h("canvas");
  const lg = h("div", { class: "legend" });
  parent.append(h("div", null, h("h3", null, title), cv, lg));
  const ch = new Chart(cv, Object.assign({ legend: lg, scale: Math.max(2, window.devicePixelRatio || 1) }, opts || {}));
  S.reportCharts.push(ch);
  return ch;
}
function kvList(pairs) {
  return h("dl", { class: "kv" }, pairs.filter((p) => p[1] != null && p[1] !== "").map(([k, v]) => [h("dt", null, k), h("dd", null, v)]));
}
function buildReport(el, sess) {
  const m = sess.machine || {};
  const c = sess.config;
  el.append(h("h1", null, t("rep.h1", { title: sessTitle(sess) })),
    h("div", { class: "rsub" }, sess.id + t("rep.started", { d: fmtDate(sess.started_at) }) + (sess.finished_at ? t("rep.finished", { d: fmtDate(sess.finished_at) }) : "") +
      t("rep.state", { s: stateName(sess.state) })));
  const runsDone = sess.runs.filter((r) => r.summary).length;
  const none = t("rep.none2");
  el.append(h("section", null, h("h2", null, t("rep.cfg")), h("div", { class: "grid2" },
    kvList([[t("rep.kind"), kindName(sess.kind) + (sess.sweep ? ": " + paramName(sess.sweep.param) + " = " + sess.sweep.values.map((v) => paramShort(sess.sweep.param, v)).join(", ") : "")],
      [t("rep.impls"), [...new Set(sess.runs.map((r) => implMeta(r.impl).label))].join(", ")], [t("rep.runs"), t("rep.runsV", { d: runsDone, n: sess.runs.length })],
      [t("rep.n"), fmtCount(c.n)], [t("rep.value"), fmtBytes(c.value_size)], [t("rep.reads"), fmtPct(c.read_ratio * 100, 1)], [t("rep.goroutines"), c.goroutines],
      [t("rep.keys"), c.dist === "zipf" ? "Zipf s = " + dec(c.zipf_s) : t("dist.uniform")],
      [t("rep.garbage"), c.garbage_rate > 0 ? dec(c.garbage_rate) + " × " + fmtBytes(c.garbage_bytes) : none],
      ["GOMEMLIMIT", c.memlimit_mib > 0 ? c.memlimit_mib + t("u.MiB") : none], [t("rep.maxmem"), c.maxmem_mib > 0 ? c.maxmem_mib + t("u.MiB") : null],
      [t("rep.fill"), c.fill_on_miss ? t("rep.yes") : null], [t("rep.profile"), profileName(c.profile)],
      [t("rep.duration"), t("rep.durV", { d: trim(c.duration_s, 1), w: trim(c.warmup_s, 1) })]]),
    kvList([["CPU", m.cpu_model || t("rep.unknown")], [t("rep.cpus"), m.logical_cpus], ["RAM", m.total_ram_bytes ? fmtBytes(m.total_ram_bytes) : t("rep.unknown")], [t("rep.os"), m.os],
      [t("rep.host"), m.hostname], [t("rep.goHarness"), sess.runs.map((r) => r.summary && r.summary.go_version).find(Boolean) || (sess.bench && sess.bench.go_version)],
      ["goarena", sess.runs.map((r) => r.summary && r.summary.goarena_version).find(Boolean)],
      ["cgo", sess.bench ? (sess.bench.cgo === "1" ? t("rep.cgoOn") : sess.bench.cgo === "0" ? t("rep.cgoOff") : t("rep.unknown")) : null],
      [t("rep.lab"), t("rep.labV", { v: m.lab_go_version || "" })]]))));
  const isSweep = sess.kind === "sweep" && sess.sweep;
  if (isSweep) {
    const sec = h("section", null, h("h2", null, t("rep.vs", { p: paramName(sess.sweep.param) })));
    const grid = h("div", { class: "rcharts" });
    sec.append(grid);
    el.append(sec);
    const charts = [];
    for (const [k, title] of [["ops", t("cm.ops")], ["p999", t("cm.p999")], ["gc", t("cm.gc")], ["rss", t("cm.rss")]]) charts.push([k, reportChart(grid, title, { xCats: [] })]);
    renderSweepCharts(sess, charts);
    const kl = h("ul", { class: "knees" });
    renderKnees(kl, sess);
    el.append(h("section", null, h("h2", null, t("rep.drops")), kl));
  } else {
    const sec = h("section", null, h("h2", null, t("rep.ts")));
    const grid = h("div", { class: "rcharts" });
    sec.append(grid);
    el.append(sec);
    const runs = sess.runs, mk = timelineMarkers(runs, "measure");
    const xo = { tipX, xFmt: xAxisSec, xTitle: t("x.measure") };
    reportChart(grid, t("cm.ops"), xo).set(lineSeries(sess, runs, (p) => p.phase === "measure" ? p.ops_per_sec / 1e6 : null, "measure"), mk);
    reportChart(grid, t("rep.p999log"), Object.assign({ logY: true }, xo)).set(lineSeries(sess, runs, (p) => p.p999_ns > 0 ? p.p999_ns / 1e3 : null, "measure"), mk);
    reportChart(grid, t("cm.gc"), xo).set(lineSeries(sess, runs, (p) => p.gc_cpu_percent, "measure"), mk);
    reportChart(grid, t("cm.rss"), xo).set(lineSeries(sess, runs, (p) => p.rss_bytes > 0 ? p.rss_bytes / MiB : null, "measure"), mk);
  }
  const impls = sess.runs.map((r) => r.impl);
  const base = impls.includes("gomap") ? "gomap" : impls.includes("gobytes") ? "gobytes" : null;
  el.append(h("section", { class: "pb" }, h("h2", null, t("rep.cmp"), base ? h("small", null, t("rep.factors", { b: implMeta(base).label })) : null),
    h("div", { class: "table-wrap" }, comparisonTable(sess, base))));
  const cdfSec = h("section", null, h("h2", null, t("rep.lat")));
  const cg = h("div", { class: "rcharts" });
  cdfSec.append(cg);
  el.append(cdfSec);
  reportChart(cg, t("rep.cdf"), Object.assign({ tipX: (x) => fmtUs(x) + t("u.us"), xFmt: fmtUs }, cdfOpts("cdf"))).set(cdfSeries(sess, "cdf"));
  reportChart(cg, t("rep.tail"), Object.assign({ tipX: (x) => fmtUs(x) + t("u.us"), xFmt: fmtUs }, cdfOpts("ccdf"))).set(cdfSeries(sess, "ccdf"));
  const withStages = sess.runs.filter((r) => r.summary && r.summary.stages && r.summary.stages.length);
  if (withStages.length) el.append(h("section", null, h("h2", null, t("rep.stages", { p: profileName(c.profile) })), h("div", { class: "table-wrap" }, stagesTable(withStages, sess))));
  const failed = sess.runs.filter((r) => r.state === "failed");
  if (failed.length) el.append(h("section", null, h("h2", null, t("rep.failed")), h("ul", null, failed.map((r) => h("li", null, runLabel(sess, r) + ": " + r.error)))));
  el.append(h("div", { class: "foot" }, t("rep.foot", { d: fmtDate(new Date().toISOString()), id: sess.id })));
}

// ------------------------------------------------------------------ Overview
function renderOverview() {
  const i = S.info;
  if (!i) return;
  const m = i.machine, tc = i.toolchain || {}, b = i.bench || {};
  const dl = $("ov-machine"); clear(dl);
  const kv = (el, pairs) => { for (const [k, v] of pairs) if (v != null && v !== "") el.append(h("dt", null, k), h("dd", null, v)); };
  kv(dl, [["CPU", m.cpu_model || t("ov.unknown")], [t("ov.cores"), m.logical_cpus],
    [t("ov.ram"), m.total_ram_bytes ? fmtBytes(m.total_ram_bytes) + (m.avail_ram_bytes ? t("ov.free", { v: fmtBytes(m.avail_ram_bytes) }) : "") : t("ov.unknown")],
    [t("ov.os"), m.os + " · " + m.goos + "/" + m.goarch], [t("ov.host"), m.hostname], [t("ov.labBuilt"), m.lab_go_version], [t("ov.toolchain"), tc.go_version || tc.error || "…"],
    [t("ov.cgoEnv"), tc.cgo_enabled ? tc.cgo_enabled + (tc.cc ? " · CC " + tc.cc : "") : "…"], [t("ov.budget"), t("ov.budgetV", { v: fmtMiB(i.budget_mib) })],
    [t("ov.results"), i.results_dir], [t("ov.history"), i.history_dir]]);
  const bl = $("ov-bench"); clear(bl);
  const yes = (v) => h("span", { class: v ? "ok" : "warn" }, v ? t("ov.yes") : t("ov.no"));
  const stateEl = h("span", { class: b.state === "ready" ? "ok" : b.state === "building" ? "" : "warn" }, b.state ? tk("bench.", b.state) : "…");
  kv(bl, [[t("ov.binary"), h("code", null, b.path || "–")], [t("ov.state"), stateEl], b.error ? [t("ov.error"), h("span", { class: "warn" }, trServer(b.error))] : ["", ""],
    ["cgo", b.cgo === "1" ? h("span", { class: "ok" }, t("ov.cgoOn")) : b.cgo === "0" ? h("span", { class: "warn" }, t("ov.cgoOff")) : t("ov.unknown")],
    [t("ov.builtWith"), b.go_version], [t("ov.module"), b.module_version], [t("ov.modified"), b.mod_time ? fmtDate(b.mod_time) : null],
    [t("ov.managed"), b.managed ? t("ov.managedLab") : t("ov.managedFlag")],
    [t("ov.progress"), yes(b.progress)], [t("ov.profiles"), yes(b.profiles)], ["goarena-evict", yes(b.evict)], [t("ov.fill"), yes(b.fill_on_miss)]]);
  const log = $("ov-buildlog");
  log.textContent = b.build_log || "";
  log.classList.toggle("hidden", !b.build_log);
  $("ov-rebuild").textContent = b.managed ? t("ov.rebuild") : t("ov.reprobe");
  $("ov-rebuild").disabled = b.state === "building";
  const docs = $("ov-docs"); clear(docs);
  for (const d of i.docs || []) docs.append(h("li", null, h("a", { href: d.url, target: "_blank", rel: "noopener" }, tx("doc." + d.name, d.title)), " ", h("code", { class: "hint" }, d.name)));
  const ig = $("ov-impls"); clear(ig);
  for (const im of i.implementations) ig.append(h("div", { class: "impl-card", style: "--c:" + im.color }, h("h3", null, im.label, h("code", null, im.name)), h("p", null, implDesc(im))));
}

// ------------------------------------------------------------------ router & init
const PAGES = ["overview", "run", "sweep", "results", "report"];
function route() {
  const m = location.hash.match(/^#\/(\w+)(?:\?(.*))?$/);
  const page = m && PAGES.includes(m[1]) ? m[1] : "overview";
  const q = new URLSearchParams((m && m[2]) || "");
  // #/...?lang=uk|en chooses the language for this view (not stored)
  if (LANGS.includes(q.get("lang")) && q.get("lang") !== LANG) setLang(q.get("lang"));
  // deep links (screenshots, sharing): #/results?id=..., #/report?id=..., #/run?live=1[&x=full]
  if (q.get("id") && page === "results") { S.resultsId = q.get("id"); S.resultsIdPinned = true; fillSessionSelects(); }
  if (q.get("id") && page === "report") { S.reportId = q.get("id"); S.reportIdPinned = true; fillSessionSelects(); }
  if ((page === "run" || page === "sweep") && q.has("live")) setSettingsCollapsed(true, page);
  if (q.get("x") === "full") S.xMode = "full";
  // #/run?preset=baseline-10m, #/sweep?preset=goroutine-sweep: fill the form (never starts a session)
  if (q.get("preset") && S.presetHash !== location.hash) {
    S.presetHash = location.hash;
    const pr = S.presets.find((x) => x.id === q.get("preset"));
    if (pr) {
      applyPreset(pr, true);
      const want = pr.kind === "sweep" ? "sweep" : "run";
      if (page !== want) { location.hash = "#/" + want + "?preset=" + pr.id; return; }
    } else toast(t("toast.unknownPreset", { id: q.get("preset") }), true);
  }
  S.page = page;
  for (const p of PAGES) $("page-" + p).classList.toggle("hidden", p !== page);
  for (const a of document.querySelectorAll("#nav a")) a.classList.toggle("active", a.dataset.page === page);
  setDocTitle();
  if (page === "run") renderRunLive();
  if (page === "sweep") {
    renderSweepLive();
    if (q.get("id") || !(S.live && S.live.session.kind === "sweep")) showStoredSweep(q.get("id"));
  }
  if (page === "results") { renderHistoryTable(); loadResults(); if (!S.historical) loadHistorical(false); }
  if (page === "report") loadReport();
  for (const c of Object.values(S.charts)) c.request();
}
function setDocTitle() {
  document.title = t("doc.title", { page: t("nav." + S.page) });
}

async function init() {
  captureStatic();
  applyStatic();
  initLangSwitch();
  try {
    const [info, presets] = await Promise.all([api("GET", "/api/info"), api("GET", "/api/presets")]);
    S.info = info; S.presets = presets;
  } catch (e) {
    document.querySelector("main").prepend(h("div", { class: "card empty" }, t("err.server", { e: e.message })));
    return;
  }
  for (const im of S.info.implementations) { S.impls[im.name] = im; S.implOrder.push(im.name); }
  renderOverview();
  initRunPage();
  initSweepPage();
  initResultsPage();
  renderPresets();
  const def = S.presets.find((p) => p.id === "baseline-10m");
  if (def) { const c = Object.assign({}, def.config, { n: 1000000, duration_s: 5, warmup_s: 1 }); runForm.set(c, null); }
  const sw = S.presets.find((p) => p.id === "goroutine-sweep");
  if (sw) sweepForm.set(sw.config, null);
  onSweepParam(false);
  $("ov-rebuild").addEventListener("click", async () => {
    try { S.info.bench = await api("POST", "/api/rebuild"); renderOverview(); toast(t("toast.rebuilding")); }
    catch (e) { toast(errText(e), true); }
  });
  $("rep-session").addEventListener("change", (e) => { S.reportId = e.target.value; S.reportIdPinned = true; loadReport(); });
  $("rep-print").addEventListener("click", () => window.print());
  window.addEventListener("beforeprint", () => { for (const c of S.reportCharts) c.draw(); });
  window.addEventListener("hashchange", route);
  setInterval(() => {
    // keep the "elapsed" counter and the toolchain info fresh
    if (S.live && S.live.session.state === "running" && (S.page === "run" || S.page === "sweep")) scheduleLive();
    if (S.info && (!S.info.toolchain || !S.info.toolchain.go_version) && S.page === "overview") {
      api("GET", "/api/info").then((i) => { S.info.toolchain = i.toolchain; S.info.bench = i.bench; renderOverview(); }).catch(() => {});
    }
  }, 1000);
  setLang(LANG); // toggles, hints and other labels set outside the static HTML
  await refreshHistory();
  if (/[?&]snap=1/.test(location.hash)) await loadSnapshot(); else connectEvents();
  route();
}
LANG = langFromUrl() || storedLang() || "uk";
init();
})();
