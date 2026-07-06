import {deploy, store} from '../../wailsjs/go/models';

// Client-inlined env prefixes: frameworks bake these into the browser bundle at
// build time, and enforce the prefix as a gate (a var without it is never
// exposed to client code). So the prefix is self-identifying — it tells us both
// that the var is build-time and which framework's rule applies — which is why
// we don't need to sniff the Dockerfile to detect the framework. Order matters:
// more specific prefixes must come before broader ones (NEXT_PUBLIC_ before the
// broad SvelteKit PUBLIC_).
const CLIENT_INLINED_PREFIXES: {prefix: string; framework: string}[] = [
    {prefix: 'NEXT_PUBLIC_', framework: 'Next.js'},
    {prefix: 'NUXT_PUBLIC_', framework: 'Nuxt'},
    {prefix: 'REACT_APP_', framework: 'Create React App'},
    {prefix: 'EXPO_PUBLIC_', framework: 'Expo'},
    {prefix: 'GATSBY_', framework: 'Gatsby'},
    {prefix: 'VITE_', framework: 'Vite'},
    {prefix: 'VUE_APP_', framework: 'Vue CLI'},
    {prefix: 'STORYBOOK_', framework: 'Storybook'},
    {prefix: 'PUBLIC_', framework: 'SvelteKit'},
];

export type BuildEnvWarningKind = 'scope' | 'missing_arg' | 'arg_wrong_stage';

export type BuildEnvWarning = {
    key: string;
    kind: BuildEnvWarningKind;
    framework?: string;
    message: string;
    suggestion?: string;
};

function clientInlinedMatch(key: string): {prefix: string; framework: string} | null {
    for (const p of CLIENT_INLINED_PREFIXES) {
        if (key.startsWith(p.prefix)) return p;
    }
    return null;
}

function isBuildArg(scope: string): boolean {
    return scope === 'build' || scope === 'both';
}

// computeBuildEnvWarnings applies the warning policy against the *live* variable
// list (including unsaved build-arg toggles) so warnings react instantly, using
// the Dockerfile facts resolved by the backend.
//
// Two complementary checks:
//   A (scope): a client-inlined var left runtime-only never reaches the build,
//     so its value is missing from the shipped bundle.
//   B (arg):   a build arg the Dockerfile can't actually receive — either no
//     matching ARG anywhere, or an ARG declared outside the build stage (ARG is
//     stage-scoped, so it must be re-declared in the stage that runs the build).
export function computeBuildEnvWarnings(
    vars: store.EnvVar[],
    info: deploy.DockerfileBuildInfo | null,
): BuildEnvWarning[] {
    if (!info || !info.buildMode) return [];

    const declared = new Set(info.declaredArgs || []);
    const inBuildStage = new Set(info.buildStageArgs || []);
    const warnings: BuildEnvWarning[] = [];

    for (const v of vars) {
        const match = clientInlinedMatch(v.key);

        // A: client-inlined var that won't be passed at build time.
        if (match && !isBuildArg(v.scope)) {
            warnings.push({
                key: v.key,
                kind: 'scope',
                framework: match.framework,
                message: `${match.framework} inlines ${match.prefix}* variables into the client bundle at build time, but ${v.key} is runtime-only, so it won't be included in the build.`,
            });
            continue;
        }

        if (!isBuildArg(v.scope) || !info.parsed) continue;

        // B: build arg the Dockerfile can't receive.
        if (!declared.has(v.key)) {
            warnings.push({
                key: v.key,
                kind: 'missing_arg',
                framework: match?.framework,
                message: `${v.key} is passed as a build arg, but the Dockerfile never declares \`ARG ${v.key}\`, so Docker ignores it during the build.`,
                suggestion: `ARG ${v.key}`,
            });
        } else if (info.hasBuildStep && !inBuildStage.has(v.key)) {
            warnings.push({
                key: v.key,
                kind: 'arg_wrong_stage',
                framework: match?.framework,
                message: `\`ARG ${v.key}\` is declared, but not in the stage that runs your build command, so it isn't available when the app is built.`,
                suggestion: `ARG ${v.key}`,
            });
        }
    }

    return warnings;
}
