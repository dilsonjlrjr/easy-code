// Package warm compiles the standard library packages a small web application uses, so the sandbox
// image ships them in the build cache (ADR-07: warm cache, no network).
package warm

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
	"time"
)

var _ = []any{context.Background, sql.Open, json.Marshal, errors.New, fmt.Sprint, slog.Info, http.ListenAndServe, os.Getenv,
	sort.Strings, strings.TrimSpace, new(sync.Mutex), time.Now}
