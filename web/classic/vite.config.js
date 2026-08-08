/*
Copyright (C) 2025 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/

import fs from 'node:fs';
import { createRequire } from 'node:module';
import path from 'node:path';
import { fileURLToPath, pathToFileURL } from 'node:url';

import react from '@vitejs/plugin-react';
import { codeInspectorPlugin } from 'code-inspector-plugin';
import { defineConfig, transformWithEsbuild } from 'vite';

const require = createRequire(import.meta.url);
const semiPluginRequire = createRequire(
  require.resolve('@douyinfe/vite-plugin-semi/package.json'),
);
const { semiThemeLoader } = semiPluginRequire('./lib/semi-theme-loader');
const sass = semiPluginRequire('sass');
const __dirname = path.dirname(fileURLToPath(import.meta.url));

function transformPath(filePath) {
  return process.platform === 'win32'
    ? filePath.replaceAll(/[\\]+/g, '/')
    : filePath;
}

function vitePluginSemiProjectTheme(options = {}) {
  return {
    name: 'vite-plugin-semi-project-theme',
    load(id) {
      const filePath = transformPath(id);
      if (
        !/@douyinfe\/semi-(ui|icons|foundation)\/lib\/.+\.css$/.test(filePath)
      ) {
        return null;
      }

      const scssFilePath = filePath.replace(/\.css$/, '.scss');
      const originalScssRaw = fs.readFileSync(scssFilePath, 'utf-8');
      const nextScssRaw = semiThemeLoader(originalScssRaw, {
        name: options.theme,
        cssLayer: options.cssLayer,
      });

      return sass.compileString(nextScssRaw, {
        importers: [
          {
            findFileUrl(url) {
              if (url.startsWith('~')) {
                return pathToFileURL(
                  require.resolve(url.slice(1), { paths: [__dirname] }),
                );
              }

              const resolvedPath = path.resolve(
                path.dirname(scssFilePath),
                url,
              );
              if (fs.existsSync(resolvedPath)) {
                return pathToFileURL(resolvedPath);
              }

              return null;
            },
          },
        ],
        logger: sass.Logger.silent,
      }).css;
    },
  };
}

// https://vitejs.dev/config/
export default defineConfig({
  resolve: {
    alias: {
      '@': path.resolve(__dirname, './src'),
    },
  },
  plugins: [
    codeInspectorPlugin({
      bundler: 'vite',
    }),
    {
      name: 'treat-js-files-as-jsx',
      async transform(code, id) {
        if (!/src\/.*\.js$/.test(id)) {
          return null;
        }

        // Use the exposed transform from vite, instead of directly
        // transforming with esbuild
        return transformWithEsbuild(code, id, {
          loader: 'jsx',
          jsx: 'automatic',
        });
      },
    },
    react(),
    vitePluginSemiProjectTheme({
      cssLayer: true,
      theme: '@douyinfe/semi-theme-default',
    }),
  ],
  optimizeDeps: {
    force: true,
    esbuildOptions: {
      loader: {
        '.js': 'jsx',
        '.json': 'json',
      },
    },
  },
  build: {
    chunkSizeWarningLimit: 8192,
    rollupOptions: {
      output: {
        manualChunks: {
          'react-core': ['react', 'react-dom', 'react-router-dom'],
          'semi-ui': [
            '@douyinfe/semi-icons',
            '@douyinfe/semi-ui',
            'i18next',
            'react-i18next',
            'i18next-browser-languagedetector',
          ],
          tools: ['axios', 'history', 'marked'],
          'react-components': [
            'react-dropzone',
            'react-fireworks',
            'react-telegram-login',
            'react-toastify',
            'react-turnstile',
          ],
        },
      },
    },
  },
  server: {
    host: '0.0.0.0',
    proxy: {
      '/api': {
        target: 'http://localhost:3000',
        changeOrigin: true,
      },
      '/mj': {
        target: 'http://localhost:3000',
        changeOrigin: true,
      },
      '/pg': {
        target: 'http://localhost:3000',
        changeOrigin: true,
      },
    },
  },
});
