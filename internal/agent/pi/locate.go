package pi

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// Locate discovers the CLI without starting a shell or reading Pi credentials.
// GUI launches often lack the user's nvm/Homebrew PATH.
func Locate() (string, error) {
	if p, err := exec.LookPath("pi"); err == nil {
		return filepath.Abs(p)
	}
	home, _ := os.UserHomeDir()
	candidates := []string{"/opt/homebrew/bin/pi", "/usr/local/bin/pi", filepath.Join(home, ".local/bin/pi"), filepath.Join(home, ".volta/bin/pi"), filepath.Join(home, ".npm-global/bin/pi")}
	versions, _ := filepath.Glob(filepath.Join(home, ".nvm/versions/node/*/bin/pi"))
	sort.Slice(versions, func(i, j int) bool { return newerNode(versions[i], versions[j]) })
	candidates = append(candidates, versions...)
	for _, p := range candidates {
		if s, err := os.Stat(p); err == nil && !s.IsDir() && s.Mode()&0111 != 0 {
			return p, nil
		}
	}
	return "", fmt.Errorf("未找到 Pi，请先安装并配置 Pi，然后重新检测")
}

func newerNode(a, b string) bool {
	version := func(p string) []int {
		v := strings.TrimPrefix(filepath.Base(filepath.Dir(filepath.Dir(p))), "v")
		nums := []int{}
		for _, s := range strings.Split(v, ".") {
			n, _ := strconv.Atoi(s)
			nums = append(nums, n)
		}
		return nums
	}
	aa, bb := version(a), version(b)
	for i := 0; i < len(aa) && i < len(bb); i++ {
		if aa[i] != bb[i] {
			return aa[i] > bb[i]
		}
	}
	return a > b
}

func processEnvironment(executable string) []string {
	env := os.Environ()
	// Pi's npm launcher uses /usr/bin/env node. Add its sibling Node before the
	// inherited PATH, preserving any user's provider environment configuration.
	extra := filepath.Dir(executable)
	path := extra + string(os.PathListSeparator) + "/opt/homebrew/bin:/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin"
	if old := os.Getenv("PATH"); old != "" {
		path = extra + string(os.PathListSeparator) + old + ":/opt/homebrew/bin:/usr/local/bin:/usr/bin:/bin"
	}
	for i, e := range env {
		if strings.HasPrefix(e, "PATH=") {
			env[i] = "PATH=" + path
			return env
		}
	}
	return append(env, "PATH="+path)
}
