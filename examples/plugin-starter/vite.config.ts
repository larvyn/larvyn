import { defineConfig } from 'vite'

// Larvyn serves the view from the plugin's folder, so every path in the
// built page must be relative (base: './'). larvyn.json points at
// dist/index.html.
export default defineConfig({
  base: './',
  build: {
    outDir: 'dist',
    emptyOutDir: true,
    // Views can't load anything from the network, so no CDN chunks and no
    // source maps pointing elsewhere.
    sourcemap: false,
  },
})
