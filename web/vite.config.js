import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';

export default defineConfig({
    plugins: [react()],
    build: {
        // script/build and script/web-build copy web/build into the Go binary
        outDir: 'build',
        // The Go side caches everything under static/ for good, because the
        // build names those files after a hash of their content (see
        // internal/sportsmatrix/webui.go).
        assetsDir: 'static',
        // swagger-ui is most of a megabyte on its own. It is already split out
        // and only loaded on the API docs page, so the default 500kB warning
        // says nothing useful.
        chunkSizeWarningLimit: 1500,
    },
    test: {
        environment: 'jsdom',
        globals: true,
        setupFiles: './src/setupTests.js',
    },
});
