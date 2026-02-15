package logging

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type Options struct {
	Level  string // "INFO"/"DEBUG"/...
	Fmt    string // "json"(default)/"text"
	Output string // "stderr+file" / "file" / ...
	File   string // path for file output
}

type Logger struct {
	mu  sync.RWMutex
	cfg config
	lg  *log.Logger
}

func New(opts Options) (*Logger, error) {
	// 默认值
	lvl := opts.Level
	if strings.TrimSpace(lvl) == "" {
		lvl = "INFO"
	}
	fmtStr := opts.Fmt
	if strings.TrimSpace(fmtStr) == "" {
		fmtStr = "json"
	}

	lv, err1 := parseLevel(lvl)
	fm, err2 := parseFormat(fmtStr)
	outs, err3 := parseOutput(opts.Output, opts.File)

	c := config{
		level:  lv,
		fmt:    fm,
		output: outs,
		file:   strings.TrimSpace(opts.File),
	}
	l := &Logger{
		cfg: c,
		lg:  log.New(os.Stderr, "", 0),
	}
	l.rebuildWriterLocked()

	// 返回最重要的错误（不中断使用）
	if err1 != nil {
		return l, err1
	}
	if err2 != nil {
		return l, err2
	}
	return l, err3
}

func (l *Logger) IsDebug() bool {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.cfg.level == DEBUG
}

func (l *Logger) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.cfg.closer != nil {
		l.cfg.closer()
		l.cfg.closer = nil
	}
	return nil
}

func (l *Logger) rebuildWriterLocked() {
	if l.cfg.closer != nil {
		l.cfg.closer()
		l.cfg.closer = nil
	}

	var writers []io.Writer
	if l.cfg.output[outStdout] {
		writers = append(writers, os.Stdout)
	}
	if l.cfg.output[outStderr] {
		writers = append(writers, os.Stderr)
	}
	if l.cfg.output[outFile] && strings.TrimSpace(l.cfg.file) != "" {
		_ = os.MkdirAll(filepath.Dir(l.cfg.file), 0o755)
		f, err := os.OpenFile(l.cfg.file, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
		if err == nil {
			writers = append(writers, f)
			l.cfg.closer = func() { _ = f.Close() }
		}
	}
	if len(writers) == 0 {
		writers = []io.Writer{os.Stderr}
	}
	l.cfg.writer = io.MultiWriter(writers...)
	l.lg.SetOutput(l.cfg.writer)
	l.lg.SetFlags(0)
}

// --- 可选：实例级 SetXXX（如果你需要） ---
func (l *Logger) SetLogLevel(level string) error {
	lv, err := parseLevel(level)
	if err != nil {
		return err
	}
	l.mu.Lock()
	l.cfg.level = lv
	l.mu.Unlock()
	return nil
}
func (l *Logger) SetLogFmt(fmtStr string) error {
	fm, err := parseFormat(fmtStr)
	if err != nil {
		return err
	}
	l.mu.Lock()
	l.cfg.fmt = fm
	l.mu.Unlock()
	return nil
}
func (l *Logger) SetOutput(out string) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	outs, err := parseOutput(out, l.cfg.file)
	l.cfg.output = outs
	l.rebuildWriterLocked()
	return err
}
func (l *Logger) SetLogFile(path string) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.cfg.file = strings.TrimSpace(path)
	if l.cfg.output[outFile] && l.cfg.file == "" {
		l.rebuildWriterLocked()
		return fmt.Errorf("SetLogFile: output includes file but path is empty")
	}
	l.rebuildWriterLocked()
	return nil
}

// --- 实例级日志方法 ---
func (l *Logger) Debug(args ...any) { l.logWithCaller(DEBUG, args...) }
func (l *Logger) Info(args ...any)  { l.logWithCaller(INFO, args...) }
func (l *Logger) Warn(args ...any)  { l.logWithCaller(WARN, args...) }
func (l *Logger) Error(args ...any) { l.logWithCaller(ERROR, args...) }

func (l *Logger) logWithCaller(lv Level, args ...any) {
	l.mu.RLock()
	min := l.cfg.level
	fmtMode := l.cfg.fmt
	l.mu.RUnlock()

	if !enabled(lv, min) {
		return
	}

	caller := resolveCaller() // 你已修复过的扫栈函数，直接复用

	if fmtMode == JSON {
		entry := buildJSONEntry(lv, caller, args...)
		b, err := json.Marshal(entry) // orderedEntry 保序
		if err != nil {
			l.lg.Println(`{"ts":"` + time.Now().Format("2006-01-02 15:04:05") + `","lv":"ERROR","caller":"` + caller + `","msg":"marshal log failed","err":` + mustQuote(err.Error()) + `}`)
			return
		}
		l.lg.Println(string(b))
		return
	}

	l.lg.Println(buildTextLine(lv, caller, args...))
}
