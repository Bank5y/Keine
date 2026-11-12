import path from 'node:path';
import tailwindcss from '@tailwindcss/vite';
import react from '@vitejs/plugin-react';
import { defineConfig } from 'vite';

// The build goes to internal/web/dist and is embedded into the Go binary.
export default defineConfig({
  plugins: [react(), tailwindcss()],
  resolve: { alias: { '@': path.resolve(__dirname, 'src') } },
  server: {
    proxy: { '/api': { target: process.env.KEINE_API || 'http://127.0.0.1:8080', changeOrigin: true } },
  },
  build: { outDir: '../internal/web/dist', emptyOutDir: true },
});
