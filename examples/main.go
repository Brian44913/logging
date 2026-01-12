package main

import (
	"fmt"
	"os"

	"github.com/Brian44913/logging"
)

func main() {
	// 你也可以用环境变量控制：
	// export GOLOG_LOG_LEVEL=DEBUG
	// export GOLOG_LOG_FMT=text
	// export GOLOG_FILE="/tmp/my.log"
	// export GOLOG_OUTPUT="stderr+file"

	_ = os.Setenv("GOLOG_LOG_LEVEL", "") // 默认 INFO
	_ = os.Setenv("GOLOG_LOG_FMT", "")   // 默认 json
	_ = os.Setenv("GOLOG_OUTPUT", "")    // 默认 stderr
	_ = os.Setenv("GOLOG_FILE", "")

	_ = logging.ReloadFromEnv()

	name := "Jack"
	age := 18
	err := fmt.Errorf("something went wrong")

	// 默认：json + INFO（不输出 DEBUG）
	logging.Debug("this debug should NOT show by default")
	logging.Error("This is test information.")
	logging.Info("msg", "name", name, "age", age, err)

	// 传入“无 key 的 JSON 字符串” => json 格式下进入 data（不转义）
	a := `{"a":"b"}`
	arr := `["x","y"]`
	logging.Info("This is test information.", a, arr)

	// 切到 DEBUG：展示全部
	_ = logging.SetLogLevel("DEBUG")
	logging.Debug("now debug WILL show", "k", "v")

	// 切到 text：普通日志
	_ = logging.SetLogFmt("text")
	logging.Info("plain text now", "name", name, "age", age, err, a)
	logging.Debug("plain text now", "name", name, "age", age, err)
	logging.Error("This is test information.")

	// 输出到 stderr+file
	_ = logging.SetLogFile("/tmp/my-logging-example.log")
	_ = logging.SetOutput("stderr+file")
	logging.Warn("written to stderr and file", "path", "/tmp/my-logging-example.log", "err", err)
}