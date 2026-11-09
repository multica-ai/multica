/*
 * The logo wall, per landing locale: companies and universities whose people
 * have signed up for Multica, from email domains. Each logo is the
 * organization's official artwork, flattened to a one-colour alpha mask in
 * `public/images/logos` so the wall can tint it; `ratio` is the mask's
 * width / height.
 */

import type { LandingDictionaryLocale } from "../i18n";

export type CustomerLogo = { name: string; logo: string; ratio: number };

export const CUSTOMER_LOGOS: Record<LandingDictionaryLocale, CustomerLogo[]> = {
  en: [
    { name: "NVIDIA", logo: "nvidia", ratio: 5.42 },
    { name: "Microsoft", logo: "microsoft", ratio: 4.69 },
    { name: "Google", logo: "google", ratio: 3.04 },
    { name: "Amazon", logo: "amazon", ratio: 3.32 },
    { name: "Zoom", logo: "zoom", ratio: 4.44 },
    { name: "DoorDash", logo: "doordash", ratio: 8.57 },
    { name: "Shopee", logo: "shopee", ratio: 3.13 },
    { name: "SAP", logo: "sap", ratio: 2.02 },
    { name: "adidas", logo: "adidas", ratio: 1.48 },
    { name: "Automattic", logo: "automattic", ratio: 13.04 },
    { name: "n8n", logo: "n8n", ratio: 3.69 },
    { name: "Dify", logo: "dify", ratio: 2.25 },
    { name: "NYU", logo: "nyu", ratio: 2.94 },
    { name: "UC Berkeley", logo: "berkeley", ratio: 5.32 },
    { name: "University of Michigan", logo: "umich", ratio: 8.57 },
    { name: "University of Oxford", logo: "oxford", ratio: 3.25 },
    { name: "NUS", logo: "nus", ratio: 2.19 },
  ],
  zh: [
    { name: "美团", logo: "meituan", ratio: 2.83 },
    { name: "小米", logo: "xiaomi", ratio: 3.75 },
    { name: "字节跳动", logo: "bytedance", ratio: 5.77 },
    { name: "腾讯", logo: "tencent", ratio: 7.79 },
    { name: "传音", logo: "transsion", ratio: 6.32 },
    { name: "OPPO", logo: "oppo", ratio: 4.2 },
    { name: "哔哩哔哩", logo: "bilibili", ratio: 3.96 },
    { name: "蔚来", logo: "nio", ratio: 2.87 },
    { name: "阿里巴巴", logo: "alibaba", ratio: 4.88 },
    { name: "京东", logo: "jd", ratio: 3.06 },
    { name: "滴滴", logo: "didi", ratio: 3.37 },
    { name: "网易", logo: "netease", ratio: 5.16 },
    { name: "清华大学", logo: "tsinghua", ratio: 1.0 },
    { name: "北京大学", logo: "pku", ratio: 1.0 },
    { name: "上海交通大学", logo: "sjtu", ratio: 3.76 },
    { name: "中国科学技术大学", logo: "ustc", ratio: 5.52 },
    { name: "中国人民大学", logo: "ruc", ratio: 4.65 },
  ],
  ko: [
    { name: "삼성", logo: "samsung", ratio: 6.52 },
    { name: "카카오엔터테인먼트", logo: "kakaoent", ratio: 2.32 },
    { name: "카카오스타일", logo: "kakaostyle", ratio: 4.78 },
    { name: "토스", logo: "toss", ratio: 3.91 },
    { name: "신세계", logo: "shinsegae", ratio: 5.07 },
    { name: "우아한청년들", logo: "woowayouths", ratio: 3.79 },
    { name: "키즈노트", logo: "kidsnote", ratio: 6.0 },
    { name: "트리노드", logo: "treenod", ratio: 5.46 },
    { name: "플리토", logo: "flitto", ratio: 3.26 },
    { name: "동아일보", logo: "donga", ratio: 3.94 },
    { name: "서울대학교", logo: "snu", ratio: 4.07 },
    { name: "KAIST", logo: "kaist", ratio: 1.0 },
    { name: "연세대학교", logo: "yonsei", ratio: 3.29 },
    { name: "고려대학교", logo: "koreauniv", ratio: 5.1 },
    { name: "성균관대학교", logo: "skku", ratio: 4.5 },
    { name: "한양대학교", logo: "hanyang", ratio: 3.06 },
  ],
  ja: [
    { name: "楽天", logo: "rakuten", ratio: 5.88 },
    { name: "NVIDIA", logo: "nvidia", ratio: 5.42 },
    { name: "Google", logo: "google", ratio: 3.04 },
    { name: "Microsoft", logo: "microsoft", ratio: 4.69 },
    { name: "Amazon", logo: "amazon", ratio: 3.32 },
    { name: "Shopee", logo: "shopee", ratio: 3.13 },
    { name: "Zoom", logo: "zoom", ratio: 4.44 },
    { name: "Dify", logo: "dify", ratio: 2.25 },
    { name: "Ptmind", logo: "ptmind", ratio: 3.73 },
    { name: "ドリコム", logo: "drecom", ratio: 4.19 },
    { name: "Comvex", logo: "comvex", ratio: 6.0 },
    { name: "Vivid Garden", logo: "vividgarden", ratio: 6.59 },
    { name: "中央大学", logo: "chuo", ratio: 6.45 },
  ],
};
