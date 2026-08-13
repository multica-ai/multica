const SEMVER_RE = /^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)(?:-((?:0|[1-9]\d*|\d*[A-Za-z-][0-9A-Za-z-]*)(?:\.(?:0|[1-9]\d*|\d*[A-Za-z-][0-9A-Za-z-]*))*))?(?:\+[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?$/;

interface ParsedVersion {
  numeric: [string, string, string];
  prerelease: string[];
}

function parseVersion(version: string): ParsedVersion | null {
  const match = SEMVER_RE.exec(version);
  if (!match) return null;
  return {
    numeric: [match[1]!, match[2]!, match[3]!],
    prerelease: match[4]?.split(".") ?? [],
  };
}

function compareNumericIdentifier(left: string, right: string): number {
  const normalizedLeft = left.replace(/^0+/, "") || "0";
  const normalizedRight = right.replace(/^0+/, "") || "0";
  if (normalizedLeft.length !== normalizedRight.length) {
    return normalizedLeft.length - normalizedRight.length;
  }
  return normalizedLeft < normalizedRight ? -1 : normalizedLeft > normalizedRight ? 1 : 0;
}

/** Compare two validated Plugin manifest versions using SemVer precedence. */
export function comparePluginVersions(left: string, right: string): number {
  const parsedLeft = parseVersion(left);
  const parsedRight = parseVersion(right);
  if (!parsedLeft || !parsedRight) {
    return left < right ? -1 : left > right ? 1 : 0;
  }

  for (let index = 0; index < parsedLeft.numeric.length; index += 1) {
    const comparison = compareNumericIdentifier(parsedLeft.numeric[index]!, parsedRight.numeric[index]!);
    if (comparison !== 0) return comparison;
  }

  const leftPrerelease = parsedLeft.prerelease;
  const rightPrerelease = parsedRight.prerelease;
  if (leftPrerelease.length === 0 && rightPrerelease.length === 0) return 0;
  if (leftPrerelease.length === 0) return 1;
  if (rightPrerelease.length === 0) return -1;

  for (let index = 0; index < leftPrerelease.length && index < rightPrerelease.length; index += 1) {
    const leftIdentifier = leftPrerelease[index]!;
    const rightIdentifier = rightPrerelease[index]!;
    const leftIsNumeric = /^\d+$/.test(leftIdentifier);
    const rightIsNumeric = /^\d+$/.test(rightIdentifier);
    if (leftIsNumeric && rightIsNumeric) {
      const comparison = compareNumericIdentifier(leftIdentifier, rightIdentifier);
      if (comparison !== 0) return comparison;
    } else if (leftIsNumeric) {
      return -1;
    } else if (rightIsNumeric) {
      return 1;
    } else if (leftIdentifier !== rightIdentifier) {
      return leftIdentifier < rightIdentifier ? -1 : 1;
    }
  }
  return leftPrerelease.length - rightPrerelease.length;
}
