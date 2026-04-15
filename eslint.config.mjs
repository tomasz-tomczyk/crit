export default [
  {
    files: ["frontend/app.js"],
    rules: {
      "no-var": "error",
      "prefer-const": "error",
      "no-unused-vars": ["error", { "args": "all", "argsIgnorePattern": "^_" }],
      "no-empty-function": "warn",
      "no-implicit-globals": "error",
      "no-useless-assignment": "error",
      "eqeqeq": "error",
      "no-shadow": "warn"
    }
  }
];
