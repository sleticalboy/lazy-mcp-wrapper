package wrapper

import "strings"

func redactArgs(args []string) []string {
	out := make([]string, len(args))
	for i, arg := range args {
		out[i] = redactValue(arg)
	}
	return out
}

func redactEnv(env map[string]string) map[string]string {
	if len(env) == 0 {
		return nil
	}
	out := make(map[string]string, len(env))
	for key, value := range env {
		out[key] = redactEnvValue(key, value)
	}
	return out
}

func redactValue(value string) string {
	lower := strings.ToLower(value)
	for _, prefix := range []string{"--token=", "--api-key=", "--secret=", "token=", "api_key=", "api-key=", "secret="} {
		if idx := strings.Index(lower, prefix); idx >= 0 {
			return value[:idx+len(prefix)] + "***REDACTED***"
		}
	}
	return value
}

func redactEnvValue(key, value string) string {
	lower := strings.ToLower(key)
	if strings.Contains(lower, "token") || strings.Contains(lower, "secret") || strings.Contains(lower, "apikey") || strings.Contains(lower, "api_key") {
		if value == "" {
			return value
		}
		return "***REDACTED***"
	}
	return value
}
