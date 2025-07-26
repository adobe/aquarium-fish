/**
 * Copyright 2025 Adobe. All rights reserved.
 * This file is licensed to you under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License. You may obtain a copy
 * of the License at http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software distributed under
 * the License is distributed on an "AS IS" BASIS, WITHOUT WARRANTIES OR REPRESENTATIONS
 * OF ANY KIND, either express or implied. See the License for the specific language
 * governing permissions and limitations under the License.
 */

// Author: Sergei Parshev (@sparshev)

import { reactRouter } from "@react-router/dev/vite";
import tailwindcss from "@tailwindcss/vite";
import { defineConfig } from "vite";
import tsconfigPaths from "vite-tsconfig-paths";
import autoprefixer from 'autoprefixer'

//export default defineConfig({
//  plugins: [tailwindcss(), reactRouter(), tsconfigPaths()],
//});
export default defineConfig(({ mode }) => {
  var config = {
    plugins: [tailwindcss(), reactRouter(), tsconfigPaths()],
    css: {
      postcss: {
        plugins: [
          autoprefixer(),
        ],
      },
    },
  }
  if( mode == 'development' ) {
    config['build'] = {
      sourceMap: true,
      declaration: true,
      declarationMap: true,
      minify: false,
      cssMinify: false,
      terserOptions: {
        compress: false,
        mangle: false,
      },
    }
  }
  return config
});
