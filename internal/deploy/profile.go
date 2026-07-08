package deploy

import "strings"

// targetProfileEnv returns the platform-conventional runtime environment a
// target cloud injects into every container, so a service tagged with that
// target behaves the same when Draft runs it locally. Only engines that
// actually inject app-visible runtime vars are represented: Cloud Run hands the
// app PORT and the K_* revision identifiers; Container Apps sets its app/
// revision names. ECS and compose inject nothing an app depends on, so they
// return nil.
func targetProfileEnv(target string, in deploymentEnvInput) map[string]string {
	switch strings.TrimSpace(strings.ToLower(target)) {
	case "cloudrun":
		return map[string]string{
			"PORT":            in.ServicePort,
			"K_SERVICE":       in.ServiceName,
			"K_REVISION":      in.ServiceName + "-00001",
			"K_CONFIGURATION": in.ServiceName,
		}
	case "containerapps":
		return map[string]string{
			"CONTAINER_APP_NAME":     in.ServiceName,
			"CONTAINER_APP_REVISION": in.ServiceName + "--000001",
		}
	default:
		return nil
	}
}
