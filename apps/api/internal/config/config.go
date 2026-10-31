package config

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
)

var hostPattern = regexp.MustCompile(`^[A-Za-z0-9.-]+$`)
var databasePattern = regexp.MustCompile(`^[A-Za-z0-9_]+$`)

type Config struct {
	DatabaseURL      string
	DatabaseMaxConns int32
	Host             string
	Port             uint16
	AllowedOrigins   []string
	DatabaseTarget   string
}

func Load() (Config, error) { return Parse(os.Getenv) }

func Parse(getenv func(string) string) (Config, error) {
	c := Config{DatabaseMaxConns: 10, Host: "127.0.0.1", Port: 8080}
	c.DatabaseURL = strings.TrimSpace(getenv("DATABASE_URL"))
	if c.DatabaseURL == "" {
		return Config{}, fmt.Errorf("DATABASE_URL is required")
	}
	u, err := url.Parse(c.DatabaseURL)
	if err != nil || (u.Scheme != "postgres" && u.Scheme != "postgresql") || u.User == nil || u.User.Username() == "" || u.Hostname() == "" || !databasePattern.MatchString(strings.TrimPrefix(u.Path, "/")) || u.Fragment != "" {
		return Config{}, fmt.Errorf("invalid DATABASE_URL")
	}
	if _, ok := u.User.Password(); !ok {
		return Config{}, fmt.Errorf("invalid DATABASE_URL")
	}
	if u.Port() != "" {
		if _, err := strconv.ParseUint(u.Port(), 10, 16); err != nil {
			return Config{}, fmt.Errorf("invalid DATABASE_URL")
		}
	}
	dbPort := u.Port()
	if dbPort == "" {
		dbPort = "5432"
	}
	c.DatabaseTarget = net.JoinHostPort(u.Hostname(), dbPort) + u.Path

	if raw := getenv("DATABASE_MAX_CONNS"); raw != "" {
		n, err := strconv.ParseInt(raw, 10, 32)
		if err != nil || n < 1 || n > 100 {
			return Config{}, fmt.Errorf("DATABASE_MAX_CONNS must be between 1 and 100")
		}
		c.DatabaseMaxConns = int32(n)
	}
	if raw := getenv("HOST"); raw != "" {
		if net.ParseIP(raw) == nil && (!hostPattern.MatchString(raw) || strings.HasPrefix(raw, "-") || strings.HasSuffix(raw, "-")) {
			return Config{}, fmt.Errorf("invalid HOST")
		}
		c.Host = raw
	}
	if raw := getenv("PORT"); raw != "" {
		n, err := strconv.ParseUint(raw, 10, 16)
		if err != nil || n == 0 {
			return Config{}, fmt.Errorf("PORT must be between 1 and 65535")
		}
		c.Port = uint16(n)
	}
	if raw := getenv("ALLOWED_ORIGINS"); raw != "" {
		seen := map[string]bool{}
		for _, origin := range strings.Split(raw, ",") {
			origin = strings.TrimSpace(origin)
			u, err := url.Parse(origin)
			if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" || seen[origin] {
				return Config{}, fmt.Errorf("invalid ALLOWED_ORIGINS")
			}
			if u.Port() != "" {
				if n, err := strconv.ParseUint(u.Port(), 10, 16); err != nil || n == 0 {
					return Config{}, fmt.Errorf("invalid ALLOWED_ORIGINS")
				}
			}
			seen[origin] = true
			c.AllowedOrigins = append(c.AllowedOrigins, origin)
		}
	} else {
		return Config{}, fmt.Errorf("ALLOWED_ORIGINS is required")
	}
	return c, nil
}

func (c Config) ListenAddr() string { return net.JoinHostPort(c.Host, strconv.Itoa(int(c.Port))) }
