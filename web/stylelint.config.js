export default {
  rules: {
    // a class defined a second time elsewhere in the stylesheet fights its
    // first rule (#124: .select)
    "no-duplicate-selectors": true,
  },
};
