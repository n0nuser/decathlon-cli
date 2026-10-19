import { defineDocs, defineConfig } from 'fumadocs-mdx/config';

export const docs = defineDocs({ dir: 'content/docs' });

export default defineConfig({
  mdxOptions: {
    // Shiki themes picked to sit on the dark ground the site defaults to.
    rehypeCodeOptions: {
      themes: { light: 'github-light', dark: 'github-dark-default' },
    },
  },
});
