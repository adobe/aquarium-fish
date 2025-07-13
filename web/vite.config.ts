import { reactRouter } from "@react-router/dev/vite";
import tailwindcss from "@tailwindcss/vite";
import { defineConfig } from "vite";
import tsconfigPaths from "vite-tsconfig-paths";

//export default defineConfig({
//  plugins: [tailwindcss(), reactRouter(), tsconfigPaths()],
//});
export default defineConfig(({ mode }) => {
  var config = {
    plugins: [tailwindcss(), reactRouter(), tsconfigPaths()],
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
