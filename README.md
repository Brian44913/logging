# logging

A small logging package with env + programmatic configuration.

## Levels
- DEBUG / INFO / WARN / ERROR
- Default is INFO (DEBUG is hidden)
- `GOLOG_LOG_LEVEL=DEBUG` shows all logs.

## Output
`GOLOG_OUTPUT` supports: `stdout`, `stderr`, `file`, combined with `+`.

Examples:
- `export GOLOG_OUTPUT="stderr"`
- `export GOLOG_OUTPUT="stderr+file"`
- `export GOLOG_FILE="/path/to/app.log"`

Special rule:
- If you set only `GOLOG_FILE` (and do NOT set `GOLOG_OUTPUT`), logs go to file only (not stderr).

## Format
- `export GOLOG_LOG_FMT="text"` -> text logs
- default -> json

## Usage

```go
import "github.com/Brian44913/logging"

func main() {
    logging.SetLogLevel("DEBUG")
    logging.SetLogFmt("json")
    logging.SetLogFile("/tmp/app.log")
    logging.SetOutput("stderr+file")

    name := "Jack"
    age := 18
    err := fmt.Errorf("boom")

    logging.Info("msg", "name", name, "age", age, err)

    a := `{"a":"b"}`
    logging.Info("This is test information.", a)
}

