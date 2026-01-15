/**
 * File type icons for the file tree
 * Uses SVG icons for common file types
 */

interface IconProps {
  className?: string
}

// TypeScript icon (TS box)
export function TypeScriptIcon({ className }: IconProps) {
  return (
    <svg className={className} viewBox="0 0 16 16" fill="none">
      <rect width="16" height="16" rx="2" fill="currentColor" fillOpacity="0.2" />
      <path d="M4.5 8.5H7V9.5H5.5V12.5H4.5V8.5Z" fill="currentColor" />
      <path d="M7.5 9.5V8.5H11V9.5H9.75V12.5H8.75V9.5H7.5Z" fill="currentColor" />
    </svg>
  )
}

// JavaScript icon (JS box)
export function JavaScriptIcon({ className }: IconProps) {
  return (
    <svg className={className} viewBox="0 0 16 16" fill="none">
      <rect width="16" height="16" rx="2" fill="currentColor" fillOpacity="0.2" />
      <path
        d="M5.5 8.5V11.5C5.5 12 5.25 12.5 4.5 12.5C3.75 12.5 3.5 12 3.5 11.5H4.25C4.25 11.75 4.375 12 4.5 12C4.625 12 4.75 11.75 4.75 11.5V8.5H5.5Z"
        fill="currentColor"
      />
      <path
        d="M7 12.5C6.25 12.5 6 12 6 11.5H6.75C6.75 11.75 6.875 12 7.25 12C7.625 12 7.75 11.75 7.75 11.5C7.75 10.5 6 11 6 9.75C6 9.25 6.375 8.5 7.25 8.5C8 8.5 8.5 9 8.5 9.5H7.75C7.75 9.25 7.5 9 7.25 9C7 9 6.75 9.25 6.75 9.75C6.75 10.75 8.5 10.25 8.5 11.5C8.5 12.25 8 12.5 7 12.5Z"
        fill="currentColor"
      />
    </svg>
  )
}

// JSON icon (curly braces)
export function JsonIcon({ className }: IconProps) {
  return (
    <svg className={className} viewBox="0 0 16 16" fill="none">
      <rect width="16" height="16" rx="2" fill="currentColor" fillOpacity="0.2" />
      <path
        d="M5 5C5 4.5 5.5 4 6 4V5C5.75 5 5.75 5.25 5.75 5.5V7C5.75 7.5 5.25 8 5 8C5.25 8 5.75 8.5 5.75 9V10.5C5.75 10.75 5.75 11 6 11V12C5.5 12 5 11.5 5 11V9.5C5 9 4.5 8.5 4 8.5V7.5C4.5 7.5 5 7 5 6.5V5Z"
        fill="currentColor"
      />
      <path
        d="M11 5C11 4.5 10.5 4 10 4V5C10.25 5 10.25 5.25 10.25 5.5V7C10.25 7.5 10.75 8 11 8C10.75 8 10.25 8.5 10.25 9V10.5C10.25 10.75 10.25 11 10 11V12C10.5 12 11 11.5 11 11V9.5C11 9 11.5 8.5 12 8.5V7.5C11.5 7.5 11 7 11 6.5V5Z"
        fill="currentColor"
      />
    </svg>
  )
}

// YAML icon
export function YamlIcon({ className }: IconProps) {
  return (
    <svg className={className} viewBox="0 0 16 16" fill="none">
      <rect width="16" height="16" rx="2" fill="currentColor" fillOpacity="0.2" />
      <text x="2" y="12" fontSize="7" fill="currentColor" fontFamily="system-ui" fontWeight="600">
        yml
      </text>
    </svg>
  )
}

// Markdown icon
export function MarkdownIcon({ className }: IconProps) {
  return (
    <svg className={className} viewBox="0 0 16 16" fill="none">
      <rect width="16" height="16" rx="2" fill="currentColor" fillOpacity="0.2" />
      <path d="M3 5V11H4.5V8L6 10L7.5 8V11H9V5H7.5L6 7.5L4.5 5H3Z" fill="currentColor" />
      <path d="M11 8V5H12.5V8L14 6.5V8.5L12.5 10L11 8.5V10H9.5L11 8Z" fill="currentColor" />
    </svg>
  )
}

// CSS icon
export function CssIcon({ className }: IconProps) {
  return (
    <svg className={className} viewBox="0 0 16 16" fill="none">
      <rect width="16" height="16" rx="2" fill="currentColor" fillOpacity="0.2" />
      <text x="2" y="11" fontSize="6" fill="currentColor" fontFamily="system-ui" fontWeight="600">
        CSS
      </text>
    </svg>
  )
}

// HTML icon
export function HtmlIcon({ className }: IconProps) {
  return (
    <svg className={className} viewBox="0 0 16 16" fill="none">
      <rect width="16" height="16" rx="2" fill="currentColor" fillOpacity="0.2" />
      <text x="1" y="11" fontSize="5.5" fill="currentColor" fontFamily="system-ui" fontWeight="600">
        HTML
      </text>
    </svg>
  )
}

// Config file icon (gear)
export function ConfigIcon({ className }: IconProps) {
  return (
    <svg className={className} viewBox="0 0 16 16" fill="currentColor">
      <path d="M7.068 1.262A1 1 0 0 1 8.932 1.262L9.254 2.316A1.1 1.1 0 0 0 10.608 2.948L11.662 2.626A1 1 0 0 1 12.876 3.876L12.554 4.93A1.1 1.1 0 0 0 13.186 6.284L14.24 6.606A1 1 0 0 1 14.24 8.47L13.186 8.792A1.1 1.1 0 0 0 12.554 10.146L12.876 11.2A1 1 0 0 1 11.662 12.414L10.608 12.092A1.1 1.1 0 0 0 9.254 12.724L8.932 13.778A1 1 0 0 1 7.068 13.778L6.746 12.724A1.1 1.1 0 0 0 5.392 12.092L4.338 12.414A1 1 0 0 1 3.124 11.2L3.446 10.146A1.1 1.1 0 0 0 2.814 8.792L1.76 8.47A1 1 0 0 1 1.76 6.606L2.814 6.284A1.1 1.1 0 0 0 3.446 4.93L3.124 3.876A1 1 0 0 1 4.338 2.626L5.392 2.948A1.1 1.1 0 0 0 6.746 2.316L7.068 1.262ZM8 10A2 2 0 1 0 8 6A2 2 0 0 0 8 10Z" />
    </svg>
  )
}

// Git icon
export function GitIcon({ className }: IconProps) {
  return (
    <svg className={className} viewBox="0 0 16 16" fill="none">
      <rect width="16" height="16" rx="2" fill="currentColor" fillOpacity="0.2" />
      <circle cx="8" cy="8" r="2" fill="currentColor" />
      <path
        d="M8 4V6M8 10V12M4 8H6M10 8H12"
        stroke="currentColor"
        strokeWidth="1.5"
        strokeLinecap="round"
      />
    </svg>
  )
}

// Map file extension to icon component
export function getFileIcon(extension?: string): React.ComponentType<IconProps> | null {
  switch (extension) {
    case 'ts':
    case 'tsx':
    case 'mts':
    case 'cts':
      return TypeScriptIcon
    case 'js':
    case 'jsx':
    case 'mjs':
    case 'cjs':
      return JavaScriptIcon
    case 'json':
      return JsonIcon
    case 'yaml':
    case 'yml':
      return YamlIcon
    case 'md':
    case 'mdx':
      return MarkdownIcon
    case 'css':
    case 'scss':
    case 'sass':
    case 'less':
      return CssIcon
    case 'html':
    case 'htm':
      return HtmlIcon
    default:
      return null
  }
}

// Check if filename is a config file
export function isConfigFile(name: string): boolean {
  const configPatterns = [
    '.gitignore',
    '.gitattributes',
    '.editorconfig',
    '.prettierrc',
    '.prettierignore',
    '.eslintrc',
    '.eslintignore',
    '.npmrc',
    '.nvmrc',
    '.env',
    'tsconfig',
    'jsconfig',
    'vite.config',
    'webpack.config',
    'rollup.config',
    'babel.config',
    'jest.config',
    'vitest.config',
    'tailwind.config',
    'postcss.config'
  ]
  const lowerName = name.toLowerCase()
  return configPatterns.some((pattern) => lowerName === pattern || lowerName.startsWith(pattern))
}

// Check if file is git-related
export function isGitFile(name: string): boolean {
  return name === '.git' || name === '.gitignore' || name === '.gitattributes'
}
