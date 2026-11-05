module.exports = (policy) =>
  function privatePath(relative, directory) {
    const parts = relative.split("/");
    return (
      relative.includes(String.fromCharCode(92)) ||
      parts.some((part, i) => {
        const name = part.toLowerCase();
        return (
          !part ||
          part === "." ||
          part === ".." ||
          policy.denied_names.includes(name) ||
          (name.startsWith(".") &&
            !(
              policy.allowed_hidden_directories.includes(name) &&
              (i < parts.length - 1 || directory)
            )) ||
          policy.denied_extensions.some((extension) => name.endsWith(extension))
        );
      })
    );
  };
