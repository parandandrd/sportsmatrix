import js from '@eslint/js';
import globals from 'globals';
import reactHooks from 'eslint-plugin-react-hooks';

// eslint-plugin-react does not support ESLint 10 yet. It isn't needed for JSX:
// ESLint 10 tracks JSX references itself, so no-unused-vars sees a component
// used only as <Thing />.
export default [
    { ignores: ['build/'] },
    js.configs.recommended,
    reactHooks.configs.flat.recommended,
    {
        files: ['**/*.{js,jsx}'],
        languageOptions: {
            globals: { ...globals.browser, ...globals.vitest },
            parserOptions: { ecmaFeatures: { jsx: true } },
        },
        rules: {
            // The React Compiler-era rule: it objects to effects that copy a
            // prop into state or load data on mount, both of which work here.
            // Satisfying it means restructuring Dashboard, BoardSettings and
            // MatrixSettings; rules-of-hooks and exhaustive-deps, which catch
            // real bugs, stay on.
            'react-hooks/set-state-in-effect': 'off',
        },
    },
    {
        files: ['vite.config.js', 'eslint.config.js'],
        languageOptions: { globals: globals.node },
    },
];
