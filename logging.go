package logging

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
)

type Level int

const (
	DEBUG Level = iota
	INFO
	WARN
	ERROR
)

func (l Level) String() string {
	switch l {
	case DEBUG:
		return "DEBUG"
	case INFO:
		return "INFO"
	case WARN:
		return "WARN"
	case ERROR:
		return "ERROR"
	default:
		return "INFO"
	}
}

func parseLevel(s string) (Level, error) {
	switch strings.ToUpper(strings.TrimSpace(s)) {
	case "DEBUG":
		return DEBUG, nil
	case "INFO", "":
		return INFO, nil
	case "WARN", "WARNING":
		return WARN, nil
	case "ERROR":
		return ERROR, nil
	default:
		return INFO, fmt.Errorf("unknown log level: %q", s)
	}
}

type Format int

const (
	// 默认 JSON（你的需求）
	JSON Format = iota
	TEXT
)

func parseFormat(s string) (Format, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "json":
		return JSON, nil
	case "text", "plain":
		return TEXT, nil
	default:
		return JSON, fmt.Errorf("unknown log fmt: %q", s)
	}
}

type outputDest int

const (
	outStdout outputDest = iota
	outStderr
	outFile
)

type config struct {
	level  Level
	fmt    Format
	output map[outputDest]bool
	file   string

	writer io.Writer
	closer func()
}

var (
	mu  sync.RWMutex
	cfg = config{
		level: INFO, // 默认 INFO（不输出 DEBUG）
		fmt:   JSON, // 默认 JSON
		output: map[outputDest]bool{
			outStderr: true, // 默认 stderr
		},
	}
	stdLogger = log.New(os.Stderr, "", 0)
)

func init() {
	_ = ReloadFromEnv()
}

// ReloadFromEnv reads env vars:
// GOLOG_LOG_LEVEL, GOLOG_LOG_FMT, GOLOG_OUTPUT, GOLOG_FILE
func ReloadFromEnv() error {
	envLevel := os.Getenv("GOLOG_LOG_LEVEL")
	envFmt := os.Getenv("GOLOG_LOG_FMT")
	envOut := os.Getenv("GOLOG_OUTPUT")
	envFile := os.Getenv("GOLOG_FILE")

	lv, err1 := parseLevel(envLevel)
	if err1 != nil {
		lv = INFO
	}

	fm, err2 := parseFormat(envFmt)
	if err2 != nil {
		fm = JSON
	}

	outs, err3 := parseOutput(envOut, envFile)

	mu.Lock()
	defer mu.Unlock()

	cfg.level = lv
	cfg.fmt = fm
	cfg.file = strings.TrimSpace(envFile)
	cfg.output = outs
	rebuildWriterLocked()

	// 汇总错误（不中断）
	if err1 != nil && err3 != nil {
		return fmt.Errorf("%v; %v", err1, err3)
	}
	if err1 != nil {
		return err1
	}
	if err2 != nil {
		return err2
	}
	return err3
}

func parseOutput(outputEnv string, fileEnv string) (map[outputDest]bool, error) {
	out := map[outputDest]bool{}
	s := strings.TrimSpace(outputEnv)

	if s == "" {
		// 特殊规则：仅设置 GOLOG_FILE（且 GOLOG_OUTPUT 为空）=> 只写 file
		if strings.TrimSpace(fileEnv) != "" {
			out[outFile] = true
			return out, nil
		}
		// 默认 stderr
		out[outStderr] = true
		return out, nil
	}

	parts := strings.Split(s, "+")
	for _, p := range parts {
		switch strings.ToLower(strings.TrimSpace(p)) {
		case "stdout":
			out[outStdout] = true
		case "stderr":
			out[outStderr] = true
		case "file":
			out[outFile] = true
		case "":
			// ignore
		default:
			return map[outputDest]bool{outStderr: true}, fmt.Errorf("unknown GOLOG_OUTPUT part: %q", p)
		}
	}

	if out[outFile] && strings.TrimSpace(fileEnv) == "" {
		return out, errors.New("GOLOG_OUTPUT includes 'file' but GOLOG_FILE is empty")
	}
	return out, nil
}

func rebuildWriterLocked() {
	if cfg.closer != nil {
		cfg.closer()
		cfg.closer = nil
	}

	var writers []io.Writer
	if cfg.output[outStdout] {
		writers = append(writers, os.Stdout)
	}
	if cfg.output[outStderr] {
		writers = append(writers, os.Stderr)
	}
	if cfg.output[outFile] && strings.TrimSpace(cfg.file) != "" {
		_ = os.MkdirAll(filepath.Dir(cfg.file), 0o755)
		f, err := os.OpenFile(cfg.file, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
		if err == nil {
			writers = append(writers, f)
			cfg.closer = func() { _ = f.Close() }
		}
	}

	if len(writers) == 0 {
		writers = []io.Writer{os.Stderr}
	}

	cfg.writer = io.MultiWriter(writers...)
	stdLogger.SetOutput(cfg.writer)
	stdLogger.SetFlags(0)
}

// ----------------- 外部配置 API（等效 env） -----------------

func SetLogLevel(level string) error {
	lv, err := parseLevel(level)
	if err != nil {
		return err
	}
	mu.Lock()
	cfg.level = lv
	mu.Unlock()
	return nil
}

// fmtStr: "json" / "text"
func SetLogFmt(fmtStr string) error {
	fm, err := parseFormat(fmtStr)
	if err != nil {
		return err
	}
	mu.Lock()
	cfg.fmt = fm
	mu.Unlock()
	return nil
}

// outputStr: "stdout", "stderr+file" ...
func SetOutput(outputStr string) error {
	mu.Lock()
	defer mu.Unlock()

	outs, err := parseOutput(outputStr, cfg.file)
	cfg.output = outs
	rebuildWriterLocked()
	return err
}

func SetLogFile(path string) error {
	mu.Lock()
	defer mu.Unlock()

	cfg.file = strings.TrimSpace(path)
	if cfg.output[outFile] && cfg.file == "" {
		rebuildWriterLocked()
		return errors.New("SetLogFile: output includes file but path is empty")
	}
	rebuildWriterLocked()
	return nil
}

// ----------------- 日志 API -----------------

func Debug(args ...any) { logWithCaller(DEBUG, args...) }
func Info(args ...any)  { logWithCaller(INFO, args...) }
func Warn(args ...any)  { logWithCaller(WARN, args...) }
func Error(args ...any) { logWithCaller(ERROR, args...) }

// lvStr: "INFO"/"WARN"/...
func Log(lvStr string, args ...any) {
	lv, err := parseLevel(lvStr)
	if err != nil {
		lv = INFO
	}
	logWithCaller(lv, args...)
}

func enabled(lv Level, min Level) bool {
	// 默认 INFO：隐藏 DEBUG；若 min=DEBUG 则全部输出
	return lv >= min
}

// 关键修复：不靠固定 skip，扫栈找第一个不属于 logging 包的 frame
func resolveCaller() string {
	// 经验值：跳过 resolveCaller + logWithCaller + runtime.Callers 本身
	pcs := make([]uintptr, 32)
	n := runtime.Callers(3, pcs)
	frames := runtime.CallersFrames(pcs[:n])

	for {
		f, more := frames.Next()
		fn := f.Function

		// 跳过 runtime / log / 本包 logging 的 frame（内联与否都不影响）
		if strings.HasPrefix(fn, "runtime.") || strings.HasPrefix(fn, "log.") {
			if !more {
				break
			}
			continue
		}
		if strings.Contains(fn, "github.com/Brian44913/logging.") || strings.HasSuffix(fn, "/logging.Info") {
			if !more {
				break
			}
			continue
		}
		if strings.Contains(fn, "/logging.") { // 兜底：只要是 logging 包
			if !more {
				break
			}
			continue
		}

		// 找到外部调用点
		return fmt.Sprintf("%s:%d", filepath.Base(f.File), f.Line)

		// not reached
		// if !more { break }
	}
	return "???:0"
}

func logWithCaller(lv Level, args ...any) {
	mu.RLock()
	min := cfg.level
	fmtMode := cfg.fmt
	mu.RUnlock()

	if !enabled(lv, min) {
		return
	}

	caller := resolveCaller()

	if fmtMode == JSON {
		entry := buildJSONEntry(lv, caller, args...)
		b, err := json.Marshal(entry) // MarshalJSON() 保证顺序
		if err != nil {
			stdLogger.Println(`{"ts":"` + time.Now().Format("2006-01-02 15:04:05") + `","lv":"ERROR","caller":"` + caller + `","msg":"marshal log failed","err":` + mustQuote(err.Error()) + `}`)
			return
		}
		stdLogger.Println(string(b))
		return
	}

	stdLogger.Println(buildTextLine(lv, caller, args...))
}

// ----------------- JSON 有序输出 -----------------

type orderedEntry struct {
	Ts     string
	Lv     string
	Caller string
	Msg    any

	Extra map[string]any // msg 之后的其它字段（不含 err）
	Err   *string        // 永远最后
}

func (e orderedEntry) MarshalJSON() ([]byte, error) {
	var b strings.Builder
	b.Grow(256)
	b.WriteByte('{')

	first := true
	writeKV := func(k string, v any) error {
		if !first {
			b.WriteByte(',')
		}
		first = false

		kk, err := json.Marshal(k)
		if err != nil {
			return err
		}
		vv, err := json.Marshal(v)
		if err != nil {
			return err
		}
		b.Write(kk)
		b.WriteByte(':')
		b.Write(vv)
		return nil
	}

	// 固定顺序
	if err := writeKV("ts", e.Ts); err != nil {
		return nil, err
	}
	if err := writeKV("lv", e.Lv); err != nil {
		return nil, err
	}
	if err := writeKV("caller", e.Caller); err != nil {
		return nil, err
	}
	if err := writeKV("msg", e.Msg); err != nil {
		return nil, err
	}

	// msg 后面的字段：稳定输出（按 key 排序）
	if len(e.Extra) > 0 {
		keys := make([]string, 0, len(e.Extra))
		for k := range e.Extra {
			// 保留字段不允许在 Extra 里抢位置
			if k == "ts" || k == "lv" || k == "caller" || k == "msg" || k == "err" {
				continue
			}
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			if err := writeKV(k, e.Extra[k]); err != nil {
				return nil, err
			}
		}
	}

	// err 永远最后
	if e.Err != nil {
		if err := writeKV("err", *e.Err); err != nil {
			return nil, err
		}
	}

	b.WriteByte('}')
	return []byte(b.String()), nil
}

func buildJSONEntry(lv Level, caller string, args ...any) orderedEntry {
	msg, fields, data, errField, extra := parseArgs(args)

	extraMap := map[string]any{}

	// fields：value 若是 JSON 字符串也解析成结构（不转义）
	for k, v := range fields {
		extraMap[k] = parseJSONIfString(v)
	}

	// data
	if len(data) == 1 {
		extraMap["data"] = mustUnmarshalAny(data[0])
	} else if len(data) > 1 {
		arr := make([]any, 0, len(data))
		for _, s := range data {
			arr = append(arr, mustUnmarshalAny(s))
		}
		extraMap["data"] = arr
	}

	// args
	if len(extra) > 0 {
		extraMap["args"] = extra
	}

	var errStr *string
	if errField != nil {
		s := errField.Error()
		errStr = &s
	}

	return orderedEntry{
		Ts:     time.Now().Format("2006-01-02 15:04:05"),
		Lv:     lv.String(),
		Caller: caller,
		Msg:    parseJSONIfString(msg),
		Extra:  extraMap,
		Err:    errStr,
	}
}

// ----------------- TEXT 输出 -----------------

func buildTextLine(lv Level, caller string, args ...any) string {
	ts := time.Now().Format("2006-01-02 15:04:05")
	msg, fields, data, errField, extra := parseArgs(args)

	var b strings.Builder
	b.WriteString(ts)
	b.WriteString(" ")
	b.WriteString(lv.String())
	b.WriteString(" ")
	b.WriteString(caller)
	b.WriteString(" ")
	b.WriteString(fmt.Sprint(msg))

	for k, v := range fields {
		b.WriteString(" ")
		b.WriteString(k)
		b.WriteString("=")
		b.WriteString(formatTextValue(v))
	}

	if errField != nil {
		b.WriteString(" err=")
		b.WriteString(formatTextValue(errField.Error()))
	}

	// data：保持原始 JSON 字符串
	for _, s := range data {
		b.WriteString(" data=")
		b.WriteString(s)
	}

	if len(extra) > 0 {
		b.WriteString(" args=")
		b.WriteString(formatTextValue(extra))
	}

	return b.String()
}

// ----------------- 参数解析（支持 kv / trailing error / json data） -----------------

func parseArgs(args []any) (msg any, fields map[string]any, dataJSON []string, errField error, extra []any) {
	fields = map[string]any{}

	if len(args) == 0 {
		return "", fields, nil, nil, nil
	}

	msg = args[0]
	rest := args[1:]

	// msg 后直接给 map
	if len(rest) == 1 {
		if m, ok := rest[0].(map[string]any); ok {
			for k, v := range m {
				fields[normalizeKey(k)] = v
			}
			return msg, fields, nil, nil, nil
		}
	}

	// 先把“无 key 的 JSON 字符串”吸到 data（保持原始字符串）
	var kv []any
	for _, v := range rest {
		if s, ok := v.(string); ok {
			ss := strings.TrimSpace(s)
			if json.Valid([]byte(ss)) {
				dataJSON = append(dataJSON, ss)
				continue
			}
		}
		kv = append(kv, v)
	}

	// 再处理 trailing error（修复：error 后面跟着 json data 时，err 也能识别）
	if len(kv) > 0 {
		if e, ok := kv[len(kv)-1].(error); ok && len(kv)%2 == 1 {
			errField = e
			kv = kv[:len(kv)-1]
		}
	}

	// 解析 key/value
	for i := 0; i+1 < len(kv); i += 2 {
		key, ok := kv[i].(string)
		if !ok {
			extra = append(extra, kv[i], kv[i+1])
			continue
		}
		fields[normalizeKey(key)] = kv[i+1]
	}

	if len(kv)%2 == 1 {
		extra = append(extra, kv[len(kv)-1])
	}

	return msg, fields, dataJSON, errField, extra
}

func normalizeKey(k string) string {
	k = strings.TrimSpace(k)
	k = strings.TrimSuffix(k, ":")
	k = strings.TrimSuffix(k, "：")
	return k
}

func parseJSONIfString(v any) any {
	s, ok := v.(string)
	if !ok {
		return v
	}
	s = strings.TrimSpace(s)
	if json.Valid([]byte(s)) {
		return mustUnmarshalAny(s)
	}
	return s
}

func mustUnmarshalAny(s string) any {
	var x any
	if err := json.Unmarshal([]byte(s), &x); err != nil {
		return s
	}
	return x
}

func mustQuote(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

func formatTextValue(v any) string {
	switch x := v.(type) {
	case string:
		// 如果是 JSON 字符串，直接原样输出（不转义）
		if json.Valid([]byte(strings.TrimSpace(x))) {
			return x
		}
		return mustQuote(x)
	case error:
		return mustQuote(x.Error())
	default:
		return fmt.Sprint(x)
	}
}