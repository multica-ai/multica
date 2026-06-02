/**
 * Feishu (飞书) wordmark sigil — the twin-sail mark.
 *
 * Vector traced from ByteDance's own IconPark set (`icon-park-outline:lark`,
 * Apache-2.0), the same house that ships Feishu, so the silhouette matches
 * the official brand mark. Single-path solid fill, meant to render white on
 * the Feishu-blue (#3370FF) sign-in button.
 *
 * react-native-svg does not resolve CSS `currentColor`, so the fill color
 * must be passed explicitly (defaults to white for the blue button).
 */
import Svg, { Path } from "react-native-svg";

interface FeishuLogoProps {
  size?: number;
  color?: string;
}

export function FeishuLogo({ size = 20, color = "#ffffff" }: FeishuLogoProps) {
  return (
    <Svg width={size} height={size} viewBox="0 0 48 48">
      <Path
        fill={color}
        fillRule="evenodd"
        clipRule="evenodd"
        d="M41.072 5.994L3.31 16.52l9.075 9.294l8.414.146l9.683-9.44q-.384-.787-.384-1.318c0-.794.311-1.422.796-1.868q1.244-1.145 2.994-.342zm1.03.734L31.578 44.49l-9.294-9.075L22.137 27l9.375-9.518a2.54 2.54 0 0 0 1.664.495c.902-.05 1.485-.596 1.759-.917a2.35 2.35 0 0 0 .567-1.649a2.57 2.57 0 0 0-.52-1.464z"
      />
    </Svg>
  );
}
