export const vendoredAssets = [
  {
    path: "web/dompurify.min.js",
    sources: ["dompurify@3.4.14"],
    packagePath: "dist/purify.min.js",
    kind: "copy",
  },
  {
    path: "web/markdown-it.min.js",
    sources: ["markdown-it@15.0.1"],
    packagePath: "dist/browser/markdown-it.umd.min.js",
    kind: "copy",
  },
  {
    path: "web/mermaid.min.js",
    sources: ["mermaid@11.17.2"],
    packagePath: "dist/mermaid.min.js",
    kind: "copy",
  },
  {
    path: "web/highlight.min.js",
    sources: [
      "@highlightjs/cdn-assets@11.12.0",
      "highlightjs-heex@1.0.1",
      "highlightjs-vue@1.0.0",
      "highlightjs-astro-js@1.0.0",
    ],
    packagePath: "-",
    kind: "generated",
  },
  {
    path: "web/diff-match-patch.min.js",
    sources: ["@sanity/diff-match-patch@3.2.0", "esbuild@0.28.2"],
    packagePath: "-",
    kind: "generated",
  },
];
