export function createJiti() {
  return {
    import() {
      return Promise.reject(new Error("jiti is not available in tailgopher"));
    },
  };
}
