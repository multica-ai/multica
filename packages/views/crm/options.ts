export type LocalizedOption = {
  value: string;
  label: {
    en: string;
    zh: string;
  };
};

export type CRMIndustryOption = LocalizedOption & {
  subIndustries: LocalizedOption[];
};

export const CRM_INDUSTRY_OPTIONS: CRMIndustryOption[] = [
  { value: "Consumer Goods", label: { en: "Consumer Goods", zh: "消费品" }, subIndustries: [
    { value: "Home & Garden", label: { en: "Home & Garden", zh: "家居园艺" } },
    { value: "Kitchenware", label: { en: "Kitchenware", zh: "厨具" } },
    { value: "Pet Products", label: { en: "Pet Products", zh: "宠物用品" } },
    { value: "Personal Care", label: { en: "Personal Care", zh: "个人护理" } },
    { value: "Gifts & Crafts", label: { en: "Gifts & Crafts", zh: "礼品工艺品" } },
  ] },
  { value: "Retail", label: { en: "Retail", zh: "零售" }, subIndustries: [
    { value: "E-commerce", label: { en: "E-commerce", zh: "电商" } },
    { value: "Supermarket", label: { en: "Supermarket", zh: "超市" } },
    { value: "Specialty Store", label: { en: "Specialty Store", zh: "专卖店" } },
    { value: "Marketplace", label: { en: "Marketplace", zh: "平台市场" } },
    { value: "Distribution", label: { en: "Distribution", zh: "分销" } },
  ] },
  { value: "Manufacturing", label: { en: "Manufacturing", zh: "制造业" }, subIndustries: [
    { value: "Electronics", label: { en: "Electronics", zh: "电子" } },
    { value: "Machinery", label: { en: "Machinery", zh: "机械" } },
    { value: "Packaging", label: { en: "Packaging", zh: "包装" } },
    { value: "Textiles", label: { en: "Textiles", zh: "纺织" } },
    { value: "Plastics", label: { en: "Plastics", zh: "塑料" } },
  ] },
  { value: "Trading", label: { en: "Trading", zh: "贸易" }, subIndustries: [
    { value: "Importer", label: { en: "Importer", zh: "进口商" } },
    { value: "Exporter", label: { en: "Exporter", zh: "出口商" } },
    { value: "Wholesaler", label: { en: "Wholesaler", zh: "批发商" } },
    { value: "Agent", label: { en: "Agent", zh: "代理商" } },
    { value: "Distributor", label: { en: "Distributor", zh: "经销商" } },
  ] },
  { value: "Construction", label: { en: "Construction", zh: "建筑工程" }, subIndustries: [
    { value: "Building Materials", label: { en: "Building Materials", zh: "建材" } },
    { value: "Contractor", label: { en: "Contractor", zh: "承包商" } },
    { value: "Interior Design", label: { en: "Interior Design", zh: "室内设计" } },
    { value: "Hardware", label: { en: "Hardware", zh: "五金" } },
    { value: "Real Estate", label: { en: "Real Estate", zh: "房地产" } },
  ] },
  { value: "Food & Beverage", label: { en: "Food & Beverage", zh: "食品饮料" }, subIndustries: [
    { value: "Restaurant", label: { en: "Restaurant", zh: "餐饮" } },
    { value: "Beverage", label: { en: "Beverage", zh: "饮料" } },
    { value: "Packaged Food", label: { en: "Packaged Food", zh: "包装食品" } },
    { value: "Catering", label: { en: "Catering", zh: "餐饮服务" } },
    { value: "Grocery", label: { en: "Grocery", zh: "食品杂货" } },
  ] },
  { value: "Healthcare", label: { en: "Healthcare", zh: "医疗健康" }, subIndustries: [
    { value: "Medical Devices", label: { en: "Medical Devices", zh: "医疗器械" } },
    { value: "Clinic", label: { en: "Clinic", zh: "诊所" } },
    { value: "Pharmacy", label: { en: "Pharmacy", zh: "药房" } },
    { value: "Wellness", label: { en: "Wellness", zh: "健康护理" } },
    { value: "Hospital", label: { en: "Hospital", zh: "医院" } },
  ] },
  { value: "Technology", label: { en: "Technology", zh: "科技" }, subIndustries: [
    { value: "Software", label: { en: "Software", zh: "软件" } },
    { value: "Hardware", label: { en: "Hardware", zh: "硬件" } },
    { value: "SaaS", label: { en: "SaaS", zh: "SaaS" } },
    { value: "Telecom", label: { en: "Telecom", zh: "通信" } },
    { value: "AI", label: { en: "AI", zh: "人工智能" } },
  ] },
  { value: "Logistics", label: { en: "Logistics", zh: "物流" }, subIndustries: [
    { value: "Freight Forwarder", label: { en: "Freight Forwarder", zh: "货代" } },
    { value: "Warehousing", label: { en: "Warehousing", zh: "仓储" } },
    { value: "Courier", label: { en: "Courier", zh: "快递" } },
    { value: "Supply Chain", label: { en: "Supply Chain", zh: "供应链" } },
    { value: "Shipping", label: { en: "Shipping", zh: "海运" } },
  ] },
  { value: "Other", label: { en: "Other", zh: "其他" }, subIndustries: [
    { value: "Other", label: { en: "Other", zh: "其他" } },
  ] },
];

export function industryLabel(value: string, locale: "en" | "zh-Hans") {
  const option = CRM_INDUSTRY_OPTIONS.find((item) => item.value === value);
  if (!option) return value;
  return locale === "zh-Hans" ? option.label.zh : option.label.en;
}

export function subIndustryOptions(industry: string) {
  return CRM_INDUSTRY_OPTIONS.find((item) => item.value === industry)?.subIndustries ?? [];
}

export function optionLabel(option: LocalizedOption, locale: "en" | "zh-Hans") {
  return locale === "zh-Hans" ? option.label.zh : option.label.en;
}

export function formatDateTimeLocal(date = new Date()) {
  const pad = (value: number) => String(value).padStart(2, "0");
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}T${pad(date.getHours())}:${pad(date.getMinutes())}`;
}

export function splitTags(value: string) {
  return value.split(",").map((tag) => tag.trim()).filter(Boolean);
}

export function appendTag(value: string, tag: string) {
  const tags = splitTags(value);
  if (!tag || tags.some((item) => item.toLocaleLowerCase() === tag.toLocaleLowerCase())) return value;
  return [...tags, tag].join(", ");
}
