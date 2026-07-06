// @ts-nocheck
/**
 * Canvas Drawing Editor - 纯 JavaScript Web Component
 * 无任何框架依赖
 */

// 类型定义
export type ToolType = 'SELECT' | 'PENCIL' | 'RECTANGLE' | 'CIRCLE' | 'TEXT' | 'IMAGE' | 'LINE' | 'ARROW' | 'DOUBLE_ARROW' | 'POLYGON' | 'TRIANGLE' | 'STAR' | 'HEART' | 'DIAMOND' | 'BEZIER' | 'SMOOTH_CURVE' | 'RICH_TEXT';

export interface Point {
  x: number;
  y: number;
}

export interface BaseObject {
  id: string;
  type: string;
  x: number;
  y: number;
  color: string;
  lineWidth: number;
  visible?: boolean;  // 图层可见性
  locked?: boolean;   // 图层锁定
  rotation?: number;  // 旋转角度（弧度制，默认0）
  skewX?: number;     // X轴斜切角度（弧度制，默认0）
  skewY?: number;     // Y轴斜切角度（弧度制，默认0）
  opacity?: number;   // 透明度（0-1，默认1）
}

export interface RectObject extends BaseObject {
  type: 'RECTANGLE';
  width: number;
  height: number;
  lineStyle?: LineStyle;  // 线条样式
  fillMode?: FillMode;    // 填充模式
}

export interface CircleObject extends BaseObject {
  type: 'CIRCLE';
  radius: number;
  lineStyle?: LineStyle;  // 线条样式
  fillMode?: FillMode;    // 填充模式
}

export interface PathObject extends BaseObject {
  type: 'PATH';
  points: Point[];
}

// 热区配置接口
export interface HotzoneConfig {
  variableName: string;   // 变量名（唯一标识）
  defaultValue?: string;  // 默认值（可选）
  description?: string;   // 描述说明（可选）
}

export interface TextObject extends BaseObject {
  type: 'TEXT';
  text: string;
  fontSize: number;
  fontFamily?: string;   // 字体
  bold?: boolean;        // 加粗
  italic?: boolean;      // 斜体
  hotzone?: HotzoneConfig; // 热区配置（可选）
}

// 富文本段落样式
export interface RichTextSegment {
  text: string;           // 文本内容
  color?: string;         // 文本颜色（可选，默认继承父对象颜色）
  fontSize?: number;      // 字体大小（可选，默认继承父对象大小）
  fontFamily?: string;    // 字体（可选，默认继承父对象字体）
  bold?: boolean;         // 加粗
  italic?: boolean;       // 斜体
  underline?: boolean;    // 下划线
  strikethrough?: boolean; // 删除线
  backgroundColor?: string; // 背景色（高亮）
}

// 富文本对象
export interface RichTextObject extends BaseObject {
  type: 'RICH_TEXT';
  segments: RichTextSegment[];  // 富文本段落数组
  fontSize: number;             // 默认字体大小
  fontFamily?: string;          // 默认字体
  lineHeight?: number;          // 行高倍数（默认1.2）
  textAlign?: 'left' | 'center' | 'right';  // 文本对齐
}

// ========== Tween动画系统 ==========

// 缓动函数类型
export type EasingFunction = (t: number) => number;

// 缓动函数集合
export const Easing = {
  linear: (t: number) => t,
  easeInQuad: (t: number) => t * t,
  easeOutQuad: (t: number) => t * (2 - t),
  easeInOutQuad: (t: number) => t < 0.5 ? 2 * t * t : -1 + (4 - 2 * t) * t,
  easeInCubic: (t: number) => t * t * t,
  easeOutCubic: (t: number) => (--t) * t * t + 1,
  easeInOutCubic: (t: number) => t < 0.5 ? 4 * t * t * t : (t - 1) * (2 * t - 2) * (2 * t - 2) + 1,
  easeInElastic: (t: number) => t === 0 ? 0 : t === 1 ? 1 : -Math.pow(2, 10 * t - 10) * Math.sin((t * 10 - 10.75) * ((2 * Math.PI) / 3)),
  easeOutElastic: (t: number) => t === 0 ? 0 : t === 1 ? 1 : Math.pow(2, -10 * t) * Math.sin((t * 10 - 0.75) * ((2 * Math.PI) / 3)) + 1,
  easeInOutElastic: (t: number) => {
    const c5 = (2 * Math.PI) / 4.5;
    return t === 0 ? 0 : t === 1 ? 1 : t < 0.5
      ? -(Math.pow(2, 20 * t - 10) * Math.sin((20 * t - 11.125) * c5)) / 2
      : (Math.pow(2, -20 * t + 10) * Math.sin((20 * t - 11.125) * c5)) / 2 + 1;
  },
  easeInBounce: (t: number) => 1 - Easing.easeOutBounce(1 - t),
  easeOutBounce: (t: number) => {
    const n1 = 7.5625, d1 = 2.75;
    if (t < 1 / d1) return n1 * t * t;
    if (t < 2 / d1) return n1 * (t -= 1.5 / d1) * t + 0.75;
    if (t < 2.5 / d1) return n1 * (t -= 2.25 / d1) * t + 0.9375;
    return n1 * (t -= 2.625 / d1) * t + 0.984375;
  },
  easeInOutBounce: (t: number) => t < 0.5 ? (1 - Easing.easeOutBounce(1 - 2 * t)) / 2 : (1 + Easing.easeOutBounce(2 * t - 1)) / 2,
  easeInBack: (t: number) => { const c1 = 1.70158, c3 = c1 + 1; return c3 * t * t * t - c1 * t * t; },
  easeOutBack: (t: number) => { const c1 = 1.70158, c3 = c1 + 1; return 1 + c3 * Math.pow(t - 1, 3) + c1 * Math.pow(t - 1, 2); },
  easeInOutBack: (t: number) => {
    const c1 = 1.70158, c2 = c1 * 1.525;
    return t < 0.5 ? (Math.pow(2 * t, 2) * ((c2 + 1) * 2 * t - c2)) / 2
      : (Math.pow(2 * t - 2, 2) * ((c2 + 1) * (t * 2 - 2) + c2) + 2) / 2;
  }
};

// Tween动画配置
export interface TweenConfig {
  duration?: number;        // 动画时长（毫秒），默认 1000
  delay?: number;           // 延迟开始（毫秒），默认 0
  easing?: EasingFunction | keyof typeof Easing;  // 缓动函数
  repeat?: number;          // 重复次数，-1 表示无限循环
  yoyo?: boolean;           // 是否往返（配合 repeat 使用）
  onStart?: () => void;     // 动画开始回调
  onUpdate?: (progress: number) => void;  // 每帧更新回调
  onComplete?: () => void;  // 动画完成回调
}

// 动画属性值
export interface TweenProps {
  x?: number;
  y?: number;
  width?: number;
  height?: number;
  radius?: number;
  rotation?: number;
  opacity?: number;
  fontSize?: number;
  [key: string]: number | undefined;
}

// Tween实例
export interface TweenInstance {
  id: string;               // 动画唯一ID
  objectId: string;         // 目标对象ID
  fromProps: TweenProps;    // 起始属性
  toProps: TweenProps;      // 目标属性
  config: Required<Omit<TweenConfig, 'onStart' | 'onUpdate' | 'onComplete'>> & Pick<TweenConfig, 'onStart' | 'onUpdate' | 'onComplete'>;
  startTime: number;        // 开始时间
  currentRepeat: number;    // 当前重复次数
  isReversed: boolean;      // 是否处于反向阶段（yoyo模式）
  isStarted: boolean;       // 是否已开始
  isCompleted: boolean;     // 是否已完成
}

// ========== 触摸手势系统 ==========

// 触摸点信息
export interface TouchPoint {
  id: number;
  x: number;
  y: number;
  startX: number;
  startY: number;
  timestamp: number;
}

// 手势类型
export type GestureType = 'none' | 'tap' | 'longPress' | 'drag' | 'pinch' | 'rotate';

// 触摸状态
export interface TouchState {
  touches: Map<number, TouchPoint>;
  gestureType: GestureType;
  initialDistance: number;      // 双指初始距离（用于缩放）
  initialAngle: number;         // 双指初始角度（用于旋转）
  initialScale: number;         // 初始缩放值
  initialRotation: number;      // 初始旋转值
  longPressTimer: number | null; // 长按定时器
  lastTapTime: number;          // 上次点击时间（用于双击检测）
  velocity: Point;              // 惯性速度
}

// 图片滤镜配置
export interface ImageFilters {
  brightness?: number;  // 亮度 0-200，100为正常
  contrast?: number;    // 对比度 0-200，100为正常
  blur?: number;        // 模糊 0-20
  grayscale?: number;   // 灰度 0-100
  saturate?: number;    // 饱和度 0-200，100为正常
}

export interface ImageObject extends BaseObject {
  type: 'IMAGE';
  width: number;
  height: number;
  dataUrl: string;
  imageElement?: HTMLImageElement;
  filters?: ImageFilters;  // 滤镜配置
}

// 线条样式类型
export type LineStyle = 'solid' | 'dashed' | 'dotted';

// 填充模式类型
export type FillMode = 'stroke' | 'fill' | 'both';

// 箭头类型
export type ArrowType = 'single' | 'double' | 'none';

// 线条对象
export interface LineObject extends BaseObject {
  type: 'LINE';
  x2: number;
  y2: number;
  lineStyle?: LineStyle;  // 线条样式：实线/虚线/点线
}

// 箭头对象
export interface ArrowObject extends BaseObject {
  type: 'ARROW';
  x2: number;
  y2: number;
  lineStyle?: LineStyle;  // 线条样式
  arrowType?: ArrowType;  // 箭头类型：单向/双向/无
}

// 多边形对象
export interface PolygonObject extends BaseObject {
  type: 'POLYGON';
  sides: number;    // 边数（3=三角形，5=五边形，6=六边形等）
  radius: number;   // 外接圆半径
  rotation?: number; // 旋转角度
  lineStyle?: LineStyle;  // 线条样式
  fillMode?: FillMode;    // 填充模式
}

// 三角形对象
export interface TriangleObject extends BaseObject {
  type: 'TRIANGLE';
  radius: number;   // 外接圆半径
  lineStyle?: LineStyle;  // 线条样式
  fillMode?: FillMode;    // 填充模式
}

// 星形对象
export interface StarObject extends BaseObject {
  type: 'STAR';
  points: number;      // 星形角数（5=五角星）
  outerRadius: number; // 外半径
  innerRadius: number; // 内半径
  lineStyle?: LineStyle;  // 线条样式
  fillMode?: FillMode;    // 填充模式
}

// 心形对象
export interface HeartObject extends BaseObject {
  type: 'HEART';
  size: number;  // 心形大小
  lineStyle?: LineStyle;  // 线条样式
  fillMode?: FillMode;    // 填充模式
}

// 菱形对象
export interface DiamondObject extends BaseObject {
  type: 'DIAMOND';
  width: number;
  height: number;
  lineStyle?: LineStyle;  // 线条样式
  fillMode?: FillMode;    // 填充模式
}

// 贝塞尔曲线控制点
export interface BezierPoint {
  x: number;           // 锚点 x
  y: number;           // 锚点 y
  cp1x?: number;       // 控制点1 x（入控制点）
  cp1y?: number;       // 控制点1 y
  cp2x?: number;       // 控制点2 x（出控制点）
  cp2y?: number;       // 控制点2 y
  type: 'corner' | 'smooth' | 'symmetric';  // 点类型：角点、平滑、对称
}

// 贝塞尔曲线对象
export interface BezierObject extends BaseObject {
  type: 'BEZIER';
  points: BezierPoint[];  // 贝塞尔曲线点列表
  closed: boolean;        // 是否闭合路径
  fill?: string;          // 填充颜色（可选）
}

// 平滑曲线对象（使用 Catmull-Rom 样条）
export interface SmoothCurveObject extends BaseObject {
  type: 'SMOOTH_CURVE';
  points: Point[];        // 控制点列表
  tension?: number;       // 张力系数（0-1，默认0.5）
  closed?: boolean;       // 是否闭合路径（默认false）
}

// 组合对象
export interface GroupObject extends BaseObject {
  type: 'GROUP';
  children: CanvasObject[];
  width: number;
  height: number;
}

export type CanvasObject = RectObject | CircleObject | PathObject | TextObject | RichTextObject | ImageObject | LineObject | ArrowObject | PolygonObject | TriangleObject | StarObject | HeartObject | DiamondObject | BezierObject | SmoothCurveObject | GroupObject;

export type LangType = 'zh' | 'en';

// ========== 形状选择器面板 API ==========

// 预定义形状类型
export type PresetShapeType = 'rectangle' | 'circle' | 'triangle' | 'star' | 'heart' | 'diamond' | 'polygon' | 'ellipse' | 'roundedRect' | 'parallelogram' | 'trapezoid' | 'hexagon' | 'octagon' | 'cross' | 'arrow' | 'callout' | 'cloud' | 'lightning' | 'custom';

// 形状配置接口
export interface ShapeConfig {
  id: string;                          // 形状唯一标识
  name: string;                        // 形状名称（显示用）
  type: PresetShapeType;               // 形状类型
  // 外观属性
  fillColor?: string;                  // 填充颜色
  fillMode?: FillMode;                 // 填充模式：stroke | fill | both
  strokeColor?: string;                // 边框颜色
  strokeWidth?: number;                // 边框宽度
  strokeStyle?: LineStyle;             // 边框样式：solid | dashed | dotted
  opacity?: number;                    // 透明度 0-1
  // 文字属性（形状中心的文字）
  text?: string;                       // 文字内容
  textColor?: string;                  // 文字颜色
  fontSize?: number;                   // 字体大小
  fontFamily?: string;                 // 字体
  fontWeight?: 'normal' | 'bold';      // 字体粗细
  fontStyle?: 'normal' | 'italic';     // 字体样式
  textAlign?: 'left' | 'center' | 'right';  // 文字对齐
  // 尺寸属性
  width?: number;                      // 默认宽度
  height?: number;                     // 默认高度
  // 特殊形状属性
  cornerRadius?: number;               // 圆角半径（圆角矩形）
  sides?: number;                      // 边数（多边形）
  points?: number;                     // 角数（星形）
  innerRadius?: number;                // 内半径（星形）
  // 自定义路径（type为custom时使用）
  customPath?: string;                 // SVG path 数据
  // 图标（用于面板显示）
  icon?: string;                       // SVG 图标内容
  // 分组
  category?: string;                   // 分类名称
}

// 形状面板配置
export interface ShapePanelConfig {
  columns?: number;                    // 列数，默认4
  itemWidth?: number;                  // 每个形状项的宽度，默认60
  itemHeight?: number;                 // 每个形状项的高度，默认60
  gap?: number;                        // 间距，默认8
  maxHeight?: number;                  // 面板最大高度，默认400
}

// 工具配置接口（新的统一配置方式）
export interface ToolConfig {
  // 基础绘图工具
  pencil?: boolean;       // 画笔
  rectangle?: boolean;    // 矩形
  circle?: boolean;       // 圆形
  text?: boolean;         // 文本
  image?: boolean;        // 图片
  line?: boolean;         // 线条
  arrow?: boolean;        // 箭头
  polygon?: boolean;      // 多边形
  triangle?: boolean;     // 三角形
  star?: boolean;         // 星形
  heart?: boolean;        // 心形
  diamond?: boolean;      // 菱形
  bezier?: boolean;       // 贝塞尔曲线/钢笔工具
  smoothCurve?: boolean;  // 平滑曲线工具
  // 操作功能
  undo?: boolean;         // 撤销
  redo?: boolean;         // 重做
  // 视图控制
  zoom?: boolean;         // 缩放
  // 文件操作
  download?: boolean;     // 导出 PNG
  exportJson?: boolean;   // 导出 JSON
  importJson?: boolean;   // 导入 JSON
  clear?: boolean;        // 清空画布
  // 样式工具
  color?: boolean;        // 颜色选择器
  fontFamily?: boolean;   // 字体选择
  bold?: boolean;         // 加粗
  italic?: boolean;       // 斜体
  // 高级功能
  layers?: boolean;       // 图层管理
  group?: boolean;        // 组合/解组
  align?: boolean;        // 对齐/分布
  shapePanel?: boolean;   // 形状选择器面板
}

export interface EditorConfig {
  title?: string;
  // 新的工具配置对象
  tool?: ToolConfig;
  // 保留旧属性以向后兼容（deprecated）
  showPencil?: boolean;
  showRectangle?: boolean;
  showCircle?: boolean;
  showText?: boolean;
  showImage?: boolean;
  showZoom?: boolean;
  showDownload?: boolean;
  showExport?: boolean;
  showImport?: boolean;
  showColor?: boolean;
  showClear?: boolean;
  // 通用配置
  lang?: LangType;
  themeColor?: string;
  enableHotzone?: boolean; // 是否启用热区功能，默认false（管理员开启，用户端关闭）
  // 图片大小限制（单位：KB，0或不设置表示不限制）
  maxImageSize?: number;
}

// 国际化文本
const i18n: Record<LangType, Record<string, string>> = {
  zh: {
    select: '选择 (V)',
    pencil: '画笔 (P)',
    rectangle: '矩形 (R)',
    circle: '圆形 (O)',
    text: '文本 (T)',
    insertImage: '插入图片',
    shapes: '形状工具',
    media: '媒体工具',
    undo: '撤销 (Ctrl+Z)',
    redo: '重做 (Ctrl+Y)',
    colorPicker: '颜色选择',
    zoomOut: '缩小',
    zoomIn: '放大',
    resetZoom: '重置缩放',
    saveProject: '保存项目 (JSON)',
    loadProject: '加载项目 (JSON)',
    exportPng: '导出 PNG',
    clearCanvas: '清空画布',
    clearConfirm: '确定清空画布吗？',
    confirm: '确定',
    cancel: '取消',
    textInputHint: '按 Enter 确认，Esc 取消',
    textPlaceholder: '输入文本...',
    startCreating: '开始创作',
    selectToolHint: '选择左侧的工具开始绘制',
    // 新增图形工具
    line: '线条 (L)',
    arrow: '箭头 (A)',
    doubleArrow: '双向箭头',
    polygon: '多边形',
    triangle: '三角形',
    star: '星形',
    heart: '心形',
    diamond: '菱形',
    bezier: '钢笔',
    bezierTool: '钢笔工具',
    closePath: '闭合路径',
    smoothCurve: '平滑曲线',
    smoothCurveTool: '平滑曲线工具',
    finishCurve: '完成曲线（双击或Enter）',
    // 线条样式
    lineStyleSolid: '实线',
    lineStyleDashed: '虚线',
    lineStyleDotted: '点线',
    // 填充模式
    fillModeStroke: '描边',
    fillModeFill: '填充',
    fillModeBoth: '描边+填充',
    // 箭头类型
    arrowTypeSingle: '单向箭头',
    arrowTypeDouble: '双向箭头',
    arrowTypeNone: '无箭头',
    // 滤镜
    filters: '滤镜',
    brightness: '亮度',
    contrast: '对比度',
    blur: '模糊',
    grayscale: '灰度',
    saturate: '饱和度',
    resetFilters: '重置滤镜',
    // 斜切变换
    skew: '斜切',
    skewX: 'X轴斜切',
    skewY: 'Y轴斜切',
    resetSkew: '重置斜切',
    // 图层管理
    layers: '图层',
    layerUp: '上移图层',
    layerDown: '下移图层',
    layerTop: '置顶',
    layerBottom: '置底',
    layerVisible: '显示/隐藏',
    layerLock: '锁定/解锁',
    show: '显示',
    hide: '隐藏',
    selected: '已选择',
    multiSelected: '已选择 {count} 个对象',
    delete: '删除',
    selectAll: '全选 (Ctrl+A)',
    // 组合/解组
    group: '组合 (Ctrl+G)',
    ungroup: '解组 (Ctrl+Shift+U)',
    // 对齐/分布
    alignLeft: '左对齐',
    alignCenter: '水平居中',
    alignRight: '右对齐',
    alignTop: '顶对齐',
    alignMiddle: '垂直居中',
    alignBottom: '底对齐',
    distributeH: '水平分布',
    distributeV: '垂直分布',
    // 文本样式
    fontFamily: '字体',
    bold: '加粗',
    italic: '斜体',
    // 热区相关
    hotzoneCreate: '新建热区',
    hotzoneEdit: '编辑热区',
    hotzoneRemove: '取消热区',
    hotzoneTitle: '热区配置',
    hotzoneVariableName: '变量名',
    hotzoneVariableNamePlaceholder: '请输入变量名（如：name）',
    hotzoneDefaultValue: '默认值',
    hotzoneDefaultValuePlaceholder: '请输入默认值（可选）',
    hotzoneDescription: '描述',
    hotzoneDescriptionPlaceholder: '请输入描述说明（可选）',
    hotzoneSave: '保存',
    hotzoneCancel: '取消',
    hotzoneVariableNameRequired: '变量名不能为空',
    // 富文本
    richText: '富文本',
    richTextPlaceholder: '请输入文字',
    addSegment: '添加段落',
    // Tween动画
    tweenAnimation: '动画',
    noAnimations: '暂无动画，选择对象后使用 tweenAnimate() 方法添加动画',
    play: '播放',
    pause: '暂停',
    stop: '停止',
    addKeyframe: '添加关键帧',
    timeline: '时间线',
    // 移动端
    touchDrag: '单指拖拽',
    touchPinch: '双指缩放',
    touchRotate: '双指旋转',
    longPress: '长按选择',
    // 形状面板
    shapePanel: '形状库',
    shapePanelTitle: '选择形状',
    basicShapes: '基础形状',
    flowchartShapes: '流程图',
    arrowShapes: '箭头',
    calloutShapes: '标注',
    customShapes: '自定义',
    noShapes: '暂无形状',
    addToCanvas: '添加到画布',
    // 图片大小限制
    imageSizeExceeded: '图片大小超出限制',
    imageSizeLimit: '最大允许',
    currentImageSize: '当前图片大小',
  },
  en: {
    select: 'Select (V)',
    pencil: 'Pencil (P)',
    rectangle: 'Rectangle (R)',
    circle: 'Circle (O)',
    text: 'Text (T)',
    insertImage: 'Insert Image',
    shapes: 'Shape Tools',
    media: 'Media Tools',
    undo: 'Undo (Ctrl+Z)',
    redo: 'Redo (Ctrl+Y)',
    colorPicker: 'Color Picker',
    zoomOut: 'Zoom Out',
    zoomIn: 'Zoom In',
    resetZoom: 'Reset Zoom',
    saveProject: 'Save Project (JSON)',
    loadProject: 'Load Project (JSON)',
    exportPng: 'Export PNG',
    clearCanvas: 'Clear Canvas',
    clearConfirm: 'Clear canvas?',
    confirm: 'Confirm',
    cancel: 'Cancel',
    textInputHint: 'Press Enter to confirm, Esc to cancel',
    textPlaceholder: 'Enter text...',
    startCreating: 'Start Creating',
    selectToolHint: 'Select a tool on the left to start drawing',
    // New shape tools
    line: 'Line (L)',
    arrow: 'Arrow (A)',
    doubleArrow: 'Double Arrow',
    polygon: 'Polygon',
    triangle: 'Triangle',
    star: 'Star',
    heart: 'Heart',
    diamond: 'Diamond',
    bezier: 'Pen',
    bezierTool: 'Pen Tool',
    closePath: 'Close Path',
    smoothCurve: 'Smooth Curve',
    smoothCurveTool: 'Smooth Curve Tool',
    finishCurve: 'Finish Curve (Double-click or Enter)',
    // Line styles
    lineStyleSolid: 'Solid',
    lineStyleDashed: 'Dashed',
    lineStyleDotted: 'Dotted',
    // Fill modes
    fillModeStroke: 'Stroke',
    fillModeFill: 'Fill',
    fillModeBoth: 'Stroke+Fill',
    // Arrow types
    arrowTypeSingle: 'Single Arrow',
    arrowTypeDouble: 'Double Arrow',
    arrowTypeNone: 'No Arrow',
    // Filters
    filters: 'Filters',
    brightness: 'Brightness',
    contrast: 'Contrast',
    blur: 'Blur',
    grayscale: 'Grayscale',
    saturate: 'Saturate',
    resetFilters: 'Reset Filters',
    // Skew transform
    skew: 'Skew',
    skewX: 'Skew X',
    skewY: 'Skew Y',
    resetSkew: 'Reset Skew',
    // Layer management
    layers: 'Layers',
    layerUp: 'Move Up',
    layerDown: 'Move Down',
    layerTop: 'Bring to Front',
    layerBottom: 'Send to Back',
    layerVisible: 'Show/Hide',
    layerLock: 'Lock/Unlock',
    show: 'Show',
    hide: 'Hide',
    selected: 'Selected',
    multiSelected: '{count} objects selected',
    delete: 'Delete',
    selectAll: 'Select All (Ctrl+A)',
    // Group/Ungroup
    group: 'Group (Ctrl+G)',
    ungroup: 'Ungroup (Ctrl+Shift+U)',
    // Align/Distribute
    alignLeft: 'Align Left',
    alignCenter: 'Align Center',
    alignRight: 'Align Right',
    alignTop: 'Align Top',
    alignMiddle: 'Align Middle',
    alignBottom: 'Align Bottom',
    distributeH: 'Distribute Horizontally',
    distributeV: 'Distribute Vertically',
    // Text styles
    fontFamily: 'Font',
    bold: 'Bold',
    italic: 'Italic',
    // Hotzone related
    hotzoneCreate: 'Create Hotzone',
    hotzoneEdit: 'Edit Hotzone',
    hotzoneRemove: 'Remove Hotzone',
    hotzoneTitle: 'Hotzone Configuration',
    hotzoneVariableName: 'Variable Name',
    hotzoneVariableNamePlaceholder: 'Enter variable name (e.g., name)',
    hotzoneDefaultValue: 'Default Value',
    hotzoneDefaultValuePlaceholder: 'Enter default value (optional)',
    hotzoneDescription: 'Description',
    hotzoneDescriptionPlaceholder: 'Enter description (optional)',
    hotzoneSave: 'Save',
    hotzoneCancel: 'Cancel',
    hotzoneVariableNameRequired: 'Variable name is required',
    // Rich text
    richText: 'Rich Text',
    richTextPlaceholder: 'Enter text',
    addSegment: 'Add Segment',
    // Tween animation
    tweenAnimation: 'Animation',
    noAnimations: 'No animations. Select an object and use tweenAnimate() method to add animations',
    play: 'Play',
    pause: 'Pause',
    stop: 'Stop',
    addKeyframe: 'Add Keyframe',
    timeline: 'Timeline',
    // Mobile touch
    touchDrag: 'Drag',
    touchPinch: 'Pinch to Zoom',
    touchRotate: 'Rotate',
    longPress: 'Long Press to Select',
    // Shape panel
    shapePanel: 'Shape Library',
    shapePanelTitle: 'Select Shape',
    basicShapes: 'Basic Shapes',
    flowchartShapes: 'Flowchart',
    arrowShapes: 'Arrows',
    calloutShapes: 'Callouts',
    customShapes: 'Custom',
    noShapes: 'No shapes available',
    addToCanvas: 'Add to Canvas',
    // Image size limit
    imageSizeExceeded: 'Image size exceeded',
    imageSizeLimit: 'Maximum allowed',
    currentImageSize: 'Current image size',
  },
};

// 默认主题色
const DEFAULT_THEME_COLOR = '#5450dc';

// 默认工具配置
const defaultToolConfig: ToolConfig = {
  // 基础绘图工具
  pencil: true,
  rectangle: true,
  circle: true,
  text: true,
  image: true,
  line: true,
  arrow: true,
  polygon: true,
  triangle: true,
  star: true,
  heart: true,
  diamond: true,
  bezier: true,
  // 操作功能
  undo: true,
  redo: true,
  // 视图控制
  zoom: true,
  // 文件操作
  download: true,
  exportJson: true,
  importJson: true,
  clear: true,
  // 样式工具
  color: true,
  fontFamily: true,
  bold: true,
  italic: true,
  // 高级功能
  layers: true,
  group: true,
  align: true,
  shapePanel: true,
};

// 默认配置
const defaultConfig: EditorConfig = {
  title: 'Canvas Editor',
  tool: { ...defaultToolConfig },
  // 旧属性默认值（向后兼容）
  showPencil: true,
  showRectangle: true,
  showCircle: true,
  showText: true,
  showImage: true,
  showZoom: true,
  showDownload: true,
  showExport: true,
  showImport: true,
  showColor: true,
  showClear: true,
  lang: 'en',
  themeColor: DEFAULT_THEME_COLOR,
  enableHotzone: false, // 默认关闭热区功能
};

/**
 * Canvas Drawing Editor Web Component
 */
export class CanvasDrawingEditor extends HTMLElement {
  // Shadow DOM
  private shadow: ShadowRoot;

  // DOM 元素
  private container!: HTMLDivElement;
  private toolbar!: HTMLDivElement;
  private topBar!: HTMLDivElement;
  private canvasContainer!: HTMLDivElement;
  private canvas!: HTMLCanvasElement;
  private ctx!: CanvasRenderingContext2D;
  private textInput!: HTMLInputElement;
  private textInputContainer!: HTMLDivElement;

  // 富文本编辑器
  private richTextEditor!: HTMLDivElement;
  private richTextSegments: RichTextSegment[] = [];
  private selectedSegmentIndex: number = -1;
  private richTextPosition: Point = { x: 0, y: 0 };
  private editingRichTextId: string | null = null;

  // 时间线编辑器
  private timelineEditor!: HTMLDivElement;
  private timelineIsPlaying: boolean = false;
  private timelineCurrentTime: number = 0;
  private timelineDuration: number = 3000; // 默认3秒

  // 配置
  private config: EditorConfig = { ...defaultConfig };

  // 状态
  private objects: CanvasObject[] = [];
  private selectedId: string | null = null;
  private tool: ToolType = 'SELECT';
  private color: string = '#000000';
  private lineWidth: number = 3;
  private lineStyle: LineStyle = 'solid';
  private fillMode: FillMode = 'stroke';
  private arrowType: ArrowType = 'single';

  // 交互状态
  private isDragging: boolean = false;
  private dragStart: Point | null = null;
  private currentObject: CanvasObject | null = null;
  private dragOffset: Point = { x: 0, y: 0 };

  // 文本输入状态
  private isTextInputVisible: boolean = false;
  private textInputPos: Point = { x: 0, y: 0 };
  private textInputScreenPos: Point = { x: 0, y: 0 };
  private editingTextId: string | null = null;

  // 调整大小状态
  private isResizing: boolean = false;
  private resizeHandle: string | null = null;
  private resizeStartBounds: { x: number; y: number; width: number; height: number } | null = null;
  private resizeOriginalObject: CanvasObject | null = null;

  // 旋转状态
  private isRotating: boolean = false;
  private rotateStartAngle: number = 0;
  private rotateObjectStartRotation: number = 0;

  // 斜切状态
  private isSkewing: boolean = false;
  private skewHandle: 'top' | 'bottom' | 'left' | 'right' | null = null;
  private skewStartPos: Point = { x: 0, y: 0 };
  private skewObjectStartSkewX: number = 0;
  private skewObjectStartSkewY: number = 0;

  // 历史记录
  private history: CanvasObject[][] = [];
  private redoHistory: CanvasObject[][] = [];  // 重做历史
  private clipboard: CanvasObject | null = null;

  // 多选状态
  private selectedIds: Set<string> = new Set();  // 多选 ID 集合
  private isSelecting: boolean = false;          // 是否正在框选
  private selectionRect: { x: number; y: number; width: number; height: number } | null = null;
  private isMultiDragging: boolean = false;      // 是否正在多选拖动
  private multiDragStart: Point = { x: 0, y: 0 }; // 多选拖动起始点

  // 多对象变换原点控制
  private transformOrigin: Point | null = null;  // 自定义变换原点（null表示使用默认中心点）
  private isDraggingOrigin: boolean = false;     // 是否正在拖拽变换原点
  private isMultiRotating: boolean = false;      // 是否正在多选旋转
  private multiRotateStartAngle: number = 0;     // 多选旋转起始角度
  private multiRotateObjectsStart: Map<string, { x: number; y: number; rotation: number }> = new Map();

  // 贝塞尔曲线工具状态
  private bezierPoints: BezierPoint[] = [];           // 当前绘制的贝塞尔点
  private isBezierDrawing: boolean = false;           // 是否正在绘制贝塞尔曲线
  private bezierDraggingPoint: number = -1;           // 正在拖拽的点索引
  private bezierDraggingHandle: 'cp1' | 'cp2' | null = null;  // 正在拖拽的控制柄

  // 平滑曲线工具状态
  private smoothCurvePoints: Point[] = [];            // 当前绘制的平滑曲线点
  private isSmoothCurveDrawing: boolean = false;      // 是否正在绘制平滑曲线
  private lastClickTime: number = 0;                  // 上次点击时间（用于检测双击）
  private bezierTempPoint: BezierPoint | null = null; // 临时预览点

  // 图层面板状态
  private layerPanelVisible: boolean = false;

  // 缩放状态
  private scale: number = 1;
  private panOffset: Point = { x: 0, y: 0 };

  // 平移状态
  private isPanning: boolean = false;
  private panStart: Point = { x: 0, y: 0 };
  private isSpacePressed: boolean = false;  // 空格键按下状态

  // 绑定的事件处理器（用于移除监听）
  private boundHandleResize: () => void;
  private boundHandleKeyDown: (e: KeyboardEvent) => void;
  private boundHandleKeyUp: (e: KeyboardEvent) => void;
  private boundHandleWheel: (e: WheelEvent) => void;

  // 热区相关状态
  private contextMenu!: HTMLDivElement;
  private hotzoneDrawer!: HTMLDivElement;
  private hotzoneEditingTextId: string | null = null;
  private hotzoneData: Record<string, string> = {};

  // Tween动画系统
  private tweens: Map<string, TweenInstance> = new Map();
  private animationFrameId: number | null = null;
  private isAnimating: boolean = false;

  // 触摸手势系统
  private touchState: TouchState = {
    touches: new Map(),
    gestureType: 'none',
    initialDistance: 0,
    initialAngle: 0,
    initialScale: 1,
    initialRotation: 0,
    longPressTimer: null,
    lastTapTime: 0,
    velocity: { x: 0, y: 0 }
  };
  private readonly LONG_PRESS_DURATION = 500; // 长按触发时间（毫秒）
  private readonly PINCH_THRESHOLD = 10;      // 捏合手势阈值（像素）
  private inertiaAnimationId: number | null = null;

  // 形状选择器面板
  private shapePanelElement!: HTMLDivElement;
  private shapePanelVisible: boolean = false;
  private registeredShapes: ShapeConfig[] = [];
  private shapePanelConfig: ShapePanelConfig = {
    columns: 4,
    itemWidth: 60,
    itemHeight: 60,
    gap: 8,
    maxHeight: 400
  };
  private pendingShapeConfig: ShapeConfig | null = null;  // 待添加到画布的形状配置

  constructor() {
    super();
    this.shadow = this.attachShadow({ mode: 'open' });

    // 绑定事件处理器
    this.boundHandleResize = this.handleResize.bind(this);
    this.boundHandleKeyDown = this.handleKeyDown.bind(this);
    this.boundHandleKeyUp = this.handleKeyUp.bind(this);
    this.boundHandleWheel = this.handleWheel.bind(this);
  }

  // 观察的属性
  static get observedAttributes(): string[] {
    return [
      'title', 'tool-config', 'initial-data', 'lang', 'theme-color',
      'enable-hotzone', 'hotzone-data', 'max-image-size',
      // 旧属性（向后兼容）
      'show-pencil', 'show-rectangle', 'show-circle', 'show-text',
      'show-image', 'show-zoom', 'show-download', 'show-export', 'show-import',
      'show-color', 'show-clear', 'show-line', 'show-arrow', 'show-polygon',
      'show-undo', 'show-redo', 'show-layers', 'show-group', 'show-align',
      'show-font-family', 'show-bold', 'show-italic'
    ];
  }

  // 生命周期：连接到 DOM
  connectedCallback(): void {
    this.parseAttributes();
    this.render();
    this.setupEventListeners();
    this.initCanvas(true); // 首次初始化需要加载初始数据
  }

  // 生命周期：从 DOM 断开
  disconnectedCallback(): void {
    this.removeEventListeners();
  }

  // 生命周期：属性变化
  attributeChangedCallback(name: string, oldValue: string | null, newValue: string | null): void {
    // 对于 initial-data 和 hotzone-data，不跳过相同值的检查
    // 因为 Vue 等框架的双向绑定可能会重新设置相同的值，但期望触发更新
    const skipEqualCheck = name === 'initial-data' || name === 'hotzone-data';
    if (!skipEqualCheck && oldValue === newValue) return;

    // 处理 initial-data 属性变化
    if (name === 'initial-data' && this.canvas) {
      // 如果值为空或 null，清空画布
      if (!newValue || newValue === 'null' || newValue === '{}') {
        this.objects = [];
        this.selectedId = null;
        this.selectedIds.clear();
        this.history = [];
        this.redoHistory = [];
        this.renderCanvas();
        this.updateUI();
        return;
      }
      this.loadInitialData();
      // 动态更新时需要手动触发渲染（画布已初始化完成）
      this.renderCanvas();
      return;
    }

    // 处理 hotzone-data 属性变化（实时更新热区文本）
    if (name === 'hotzone-data' && this.canvas) {
      this.parseHotzoneData();
      this.applyHotzoneData();
      this.renderCanvas();
      return;
    }

    // 需要重新渲染 UI 的属性
    const rerenderAttributes = ['title', 'lang', 'theme-color', 'tool-config', 'enable-hotzone',
      'show-pencil', 'show-rectangle', 'show-circle', 'show-text', 'show-image', 'show-zoom',
      'show-download', 'show-export', 'show-import', 'show-color', 'show-clear', 'show-line',
      'show-arrow', 'show-polygon', 'show-undo', 'show-redo', 'show-layers', 'show-group', 'show-align'];

    this.parseAttributes();

    if (this.container && rerenderAttributes.includes(name)) {
      // 保存当前画布数据和状态
      const currentObjects = [...this.objects];
      const currentScale = this.scale;
      const currentPanOffset = { ...this.panOffset };
      const currentSelectedIds = new Set(this.selectedIds);

      // 重新渲染 UI（工具栏、样式等）
      this.render();
      this.setupEventListeners();
      this.initCanvas(false); // 不加载初始数据

      // 恢复画布数据和状态
      this.objects = currentObjects;
      this.scale = currentScale;
      this.panOffset = currentPanOffset;
      this.selectedIds = currentSelectedIds;

      // 重新加载图片元素
      this.objects.forEach(obj => {
        if (obj.type === 'IMAGE' && (obj as ImageObject).dataUrl) {
          const img = new Image();
          img.onload = () => {
            (obj as ImageObject).imageElement = img;
            this.renderCanvas();
          };
          img.src = (obj as ImageObject).dataUrl;
        }
      });

      // 重新渲染画布
      this.updateZoomDisplay();
      this.renderCanvas();
    } else if (this.container) {
      this.updateUI();
    }
  }

  // 解析 HTML 属性
  private parseAttributes(): void {
    const langAttr = this.getAttribute('lang');
    const lang: LangType = (langAttr === 'en' || langAttr === 'zh') ? langAttr : defaultConfig.lang!;

    // 解析新的 tool-config 属性
    let toolConfig: ToolConfig = { ...defaultToolConfig };
    const toolConfigAttr = this.getAttribute('tool-config');
    if (toolConfigAttr) {
      try {
        const parsed = JSON.parse(toolConfigAttr);
        toolConfig = { ...defaultToolConfig, ...parsed };
      } catch (err) {
        console.error('Failed to parse tool-config:', err);
      }
    } else {
      // 如果没有 tool-config，使用旧属性（向后兼容）
      toolConfig = {
        pencil: this.getAttribute('show-pencil') !== 'false',
        rectangle: this.getAttribute('show-rectangle') !== 'false',
        circle: this.getAttribute('show-circle') !== 'false',
        text: this.getAttribute('show-text') !== 'false',
        image: this.getAttribute('show-image') !== 'false',
        line: this.getAttribute('show-line') !== 'false',
        arrow: this.getAttribute('show-arrow') !== 'false',
        polygon: this.getAttribute('show-polygon') !== 'false',
        triangle: this.getAttribute('show-triangle') !== 'false',
        star: this.getAttribute('show-star') !== 'false',
        heart: this.getAttribute('show-heart') !== 'false',
        diamond: this.getAttribute('show-diamond') !== 'false',
        bezier: this.getAttribute('show-bezier') !== 'false',
        undo: this.getAttribute('show-undo') !== 'false',
        redo: this.getAttribute('show-redo') !== 'false',
        zoom: this.getAttribute('show-zoom') !== 'false',
        download: this.getAttribute('show-download') !== 'false',
        exportJson: this.getAttribute('show-export') !== 'false',
        importJson: this.getAttribute('show-import') !== 'false',
        clear: this.getAttribute('show-clear') !== 'false',
        color: this.getAttribute('show-color') !== 'false',
        fontFamily: this.getAttribute('show-font-family') !== 'false',
        bold: this.getAttribute('show-bold') !== 'false',
        italic: this.getAttribute('show-italic') !== 'false',
        layers: this.getAttribute('show-layers') !== 'false',
        group: this.getAttribute('show-group') !== 'false',
        align: this.getAttribute('show-align') !== 'false',
        shapePanel: this.getAttribute('show-shape-panel') !== 'false',
      };
    }

    this.config = {
      title: this.getAttribute('title') || defaultConfig.title,
      tool: toolConfig,
      // 旧属性保留（向后兼容）
      showPencil: toolConfig.pencil,
      showRectangle: toolConfig.rectangle,
      showCircle: toolConfig.circle,
      showText: toolConfig.text,
      showImage: toolConfig.image,
      showZoom: toolConfig.zoom,
      showDownload: toolConfig.download,
      showExport: toolConfig.exportJson,
      showImport: toolConfig.importJson,
      showColor: toolConfig.color,
      showClear: toolConfig.clear,
      lang: lang,
      themeColor: this.getAttribute('theme-color') || defaultConfig.themeColor,
      enableHotzone: this.getAttribute('enable-hotzone') === 'true',
      maxImageSize: this.parseMaxImageSize(),
    };

    // 解析热区数据
    this.parseHotzoneData();
  }

  // 解析图片大小限制（支持 KB 和 MB 单位）
  private parseMaxImageSize(): number {
    const attr = this.getAttribute('max-image-size');
    if (!attr) return 0;

    const value = attr.trim().toLowerCase();
    const num = parseFloat(value);

    if (isNaN(num) || num <= 0) return 0;

    // 支持 MB 单位
    if (value.endsWith('mb')) {
      return num * 1024; // 转换为 KB
    }
    // 默认为 KB
    return num;
  }

  // 获取工具配置（兼容新旧配置方式）
  private getToolConfig(): ToolConfig {
    return this.config.tool || defaultToolConfig;
  }

  // 解析热区数据属性
  private parseHotzoneData(): void {
    const hotzoneDataAttr = this.getAttribute('hotzone-data');
    if (hotzoneDataAttr) {
      try {
        this.hotzoneData = JSON.parse(hotzoneDataAttr);
      } catch (err) {
        console.error('Failed to parse hotzone-data:', err);
        this.hotzoneData = {};
      }
    }
  }

  // 获取国际化文本
  private t(key: string): string {
    const lang = this.config.lang || 'zh';
    return i18n[lang][key] || key;
  }

  // 生成唯一 ID
  private generateId(): string {
    return Math.random().toString(36).substr(2, 9);
  }

  // 精确测量文本宽度
  private measureTextWidth(text: string, fontSize: number, fontFamily: string = 'sans-serif', bold: boolean = false, italic: boolean = false): number {
    if (!this.ctx) {
      // 回退方案：粗略估算（中文字符1倍，英文0.6倍）
      let width = 0;
      for (const char of text) {
        width += char.charCodeAt(0) > 127 ? fontSize : fontSize * 0.6;
      }
      return width;
    }
    this.ctx.save();
    const boldStr = bold ? 'bold ' : '';
    const italicStr = italic ? 'italic ' : '';
    this.ctx.font = `${italicStr}${boldStr}${fontSize}px ${fontFamily}`;
    const metrics = this.ctx.measureText(text);
    this.ctx.restore();
    return metrics.width;
  }

  // 测量富文本总宽度
  private measureRichTextWidth(rt: RichTextObject): number {
    let totalWidth = 0;
    const defaultFontFamily = rt.fontFamily || 'sans-serif';

    for (const segment of rt.segments) {
      const fontSize = segment.fontSize || rt.fontSize;
      const fontFamily = segment.fontFamily || defaultFontFamily;
      const bold = segment.bold || false;
      const italic = segment.italic || false;
      totalWidth += this.measureTextWidth(segment.text, fontSize, fontFamily, bold, italic);
    }

    return totalWidth;
  }

  // 获取富文本最大字体大小
  private getRichTextMaxFontSize(rt: RichTextObject): number {
    let maxFontSize = rt.fontSize;
    for (const segment of rt.segments) {
      if (segment.fontSize && segment.fontSize > maxFontSize) {
        maxFontSize = segment.fontSize;
      }
    }
    return maxFontSize;
  }

  // ========== 路径文本辅助方法 ==========

  // 计算两点之间的距离
  private distanceBetweenPoints(p1: Point, p2: Point): number {
    return Math.sqrt(Math.pow(p2.x - p1.x, 2) + Math.pow(p2.y - p1.y, 2));
  }

  // 计算二次贝塞尔曲线上的点
  private getQuadraticBezierPoint(t: number, p0: Point, p1: Point, p2: Point): Point {
    const mt = 1 - t;
    return {
      x: mt * mt * p0.x + 2 * mt * t * p1.x + t * t * p2.x,
      y: mt * mt * p0.y + 2 * mt * t * p1.y + t * t * p2.y
    };
  }

  // 计算三次贝塞尔曲线上的点
  private getCubicBezierPoint(t: number, p0: Point, p1: Point, p2: Point, p3: Point): Point {
    const mt = 1 - t;
    const mt2 = mt * mt;
    const mt3 = mt2 * mt;
    const t2 = t * t;
    const t3 = t2 * t;
    return {
      x: mt3 * p0.x + 3 * mt2 * t * p1.x + 3 * mt * t2 * p2.x + t3 * p3.x,
      y: mt3 * p0.y + 3 * mt2 * t * p1.y + 3 * mt * t2 * p2.y + t3 * p3.y
    };
  }

  // 将屏幕坐标转换为画布坐标
  private screenToCanvas(screenX: number, screenY: number): Point {
    return {
      x: (screenX - this.panOffset.x) / this.scale,
      y: (screenY - this.panOffset.y) / this.scale
    };
  }

  // 加载初始数据
  private loadInitialData(): void {
    const initialData = this.getAttribute('initial-data');
    if (!initialData) return;

    try {
      const parsed = JSON.parse(initialData);

      // 支持两种格式：
      // 1. 对象格式：{ "objects": [...] }
      // 2. 数组格式：[...] （用户可能直接传入 e.detail.objects）
      let objectsArray: CanvasObject[];
      if (Array.isArray(parsed)) {
        // 数组格式，直接使用
        objectsArray = parsed;
      } else if (parsed.objects && Array.isArray(parsed.objects)) {
        // 对象格式，取 objects 属性
        objectsArray = parsed.objects;
      } else {
        console.error('initial-data 格式无效，需要数组或包含 objects 数组的对象');
        return;
      }

      this.objects = objectsArray;
      this.selectedId = null;

      // 每次通过 initial-data 加载数据时，重置缩放和平移
      // 确保主画布和小地图都从统一的 100% 缩放、无平移状态开始
      this.scale = 1;
      this.panOffset = { x: 0, y: 0 };
      this.updateZoomDisplay();

      // 应用热区数据（替换文本内容）
      this.applyHotzoneData();

      // 重新加载图片（异步加载完成后需要重新渲染）
      this.objects.forEach(obj => {
        if (obj.type === 'IMAGE' && (obj as ImageObject).dataUrl) {
          const img = new Image();
          img.onload = () => {
            (obj as ImageObject).imageElement = img;
            this.renderCanvas();
          };
          img.src = (obj as ImageObject).dataUrl;
        }
      });

      // 更新UI状态（隐藏空画布提示等）
      this.updateUI();
    } catch (err) {
      console.error('Failed to parse initial-data:', err);
    }
  }

  // 设置事件监听
  private setupEventListeners(): void {
    window.addEventListener('resize', this.boundHandleResize);
    window.addEventListener('keydown', this.boundHandleKeyDown);
    window.addEventListener('keyup', this.boundHandleKeyUp);
  }

  // 移除事件监听
  private removeEventListeners(): void {
    window.removeEventListener('resize', this.boundHandleResize);
    window.removeEventListener('keydown', this.boundHandleKeyDown);
    window.removeEventListener('keyup', this.boundHandleKeyUp);
    if (this.canvas) {
      this.canvas.removeEventListener('wheel', this.boundHandleWheel);
    }
  }

  // 窗口大小变化处理
  private handleResize(): void {
    this.initCanvas(false);
  }

  // 初始化画布
  private initCanvas(loadInitial: boolean = false): void {
    if (!this.canvasContainer || !this.canvas) return;

    // 使用 requestAnimationFrame 确保 DOM 已经渲染
    requestAnimationFrame(() => {
      this.canvas.width = this.canvasContainer.clientWidth;
      this.canvas.height = this.canvasContainer.clientHeight;

      // 首次初始化时加载初始数据（确保画布尺寸已设置）
      if (loadInitial) {
        this.loadInitialData();
      }

      this.renderCanvas();
    });
  }

  // Catmull-Rom 样条曲线辅助函数
  private drawCatmullRomSpline(ctx: CanvasRenderingContext2D, points: Point[], tension: number = 0.5, closed: boolean = false): void {
    if (points.length < 2) return;

    // 如果只有两个点，直接画直线
    if (points.length === 2) {
      ctx.moveTo(points[0].x, points[0].y);
      ctx.lineTo(points[1].x, points[1].y);
      return;
    }

    const pts = closed ? [...points, points[0], points[1]] : points;

    ctx.moveTo(pts[0].x, pts[0].y);

    for (let i = 0; i < pts.length - 1; i++) {
      const p0 = i === 0 ? pts[0] : pts[i - 1];
      const p1 = pts[i];
      const p2 = pts[i + 1];
      const p3 = i + 2 < pts.length ? pts[i + 2] : p2;

      // Catmull-Rom 转换为贝塞尔曲线的控制点
      const cp1x = p1.x + (p2.x - p0.x) / 6 * tension;
      const cp1y = p1.y + (p2.y - p0.y) / 6 * tension;
      const cp2x = p2.x - (p3.x - p1.x) / 6 * tension;
      const cp2y = p2.y - (p3.y - p1.y) / 6 * tension;

      ctx.bezierCurveTo(cp1x, cp1y, cp2x, cp2y, p2.x, p2.y);
    }

    if (closed) {
      ctx.closePath();
    }
  }

  // 获取鼠标在画布上的位置（考虑缩放和平移）
  private getMousePos(e: MouseEvent | TouchEvent): Point {
    const rect = this.canvas.getBoundingClientRect();
    let clientX: number, clientY: number;

    if ('touches' in e && e.touches.length > 0) {
      clientX = e.touches[0].clientX;
      clientY = e.touches[0].clientY;
    } else if ('clientX' in e) {
      clientX = e.clientX;
      clientY = e.clientY;
    } else {
      return { x: 0, y: 0 };
    }

    // Account for any CSS-to-buffer size difference (e.g. overlay mode absolute sizing,
    // device pixel ratio). rect reflects the CSS visual size; canvas.width is the buffer size.
    const cssToBuffer = rect.width > 0 ? this.canvas.width / rect.width : 1;
    const x = ((clientX - rect.left) * cssToBuffer - this.panOffset.x) / this.scale;
    const y = ((clientY - rect.top) * cssToBuffer - this.panOffset.y) / this.scale;
    return { x, y };
  }

  // 获取屏幕坐标（不考虑缩放和平移）
  private getScreenPos(e: MouseEvent | TouchEvent): Point {
    const rect = this.canvas.getBoundingClientRect();
    let clientX: number, clientY: number;

    if ('touches' in e && e.touches.length > 0) {
      clientX = e.touches[0].clientX;
      clientY = e.touches[0].clientY;
    } else if ('clientX' in e) {
      clientX = e.clientX;
      clientY = e.clientY;
    } else {
      return { x: 0, y: 0 };
    }

    const cssToBuffer = rect.width > 0 ? this.canvas.width / rect.width : 1;
    return { x: (clientX - rect.left) * cssToBuffer, y: (clientY - rect.top) * cssToBuffer };
  }

  // 获取对象边界
  private getObjectBounds(obj: CanvasObject): { x: number; y: number; width: number; height: number } {
    switch (obj.type) {
      case 'RECTANGLE':
      case 'IMAGE': {
        const r = obj as RectObject | ImageObject;
        return { x: r.x, y: r.y, width: r.width, height: r.height };
      }
      case 'CIRCLE': {
        const c = obj as CircleObject;
        return { x: c.x - c.radius, y: c.y - c.radius, width: c.radius * 2, height: c.radius * 2 };
      }
      case 'TEXT': {
        const t = obj as TextObject;
        const width = this.measureTextWidth(t.text, t.fontSize);
        // 文本现在是居中绘制的，x,y 是中心点
        return { x: t.x - width / 2, y: t.y - t.fontSize / 2, width, height: t.fontSize };
      }
      case 'RICH_TEXT': {
        const rt = obj as RichTextObject;
        const width = this.measureRichTextWidth(rt);
        const maxFontSize = this.getRichTextMaxFontSize(rt);
        const height = maxFontSize * (rt.lineHeight || 1.2);

        // 根据 textAlign 计算边界
        if (rt.textAlign === 'center') {
          // 居中对齐：x,y 是中心点
          return { x: rt.x - width / 2, y: rt.y - maxFontSize / 2, width, height };
        } else if (rt.textAlign === 'right') {
          return { x: rt.x - width, y: rt.y - maxFontSize, width, height };
        }
        return { x: rt.x, y: rt.y - maxFontSize, width, height };
      }
      case 'PATH': {
        const p = obj as PathObject;
        if (p.points.length === 0) return { x: 0, y: 0, width: 0, height: 0 };
        const minX = Math.min(...p.points.map(pt => pt.x));
        const maxX = Math.max(...p.points.map(pt => pt.x));
        const minY = Math.min(...p.points.map(pt => pt.y));
        const maxY = Math.max(...p.points.map(pt => pt.y));
        return { x: minX, y: minY, width: maxX - minX, height: maxY - minY };
      }
      case 'LINE': {
        const l = obj as LineObject;
        const minX = Math.min(l.x, l.x2);
        const maxX = Math.max(l.x, l.x2);
        const minY = Math.min(l.y, l.y2);
        const maxY = Math.max(l.y, l.y2);
        return { x: minX, y: minY, width: maxX - minX || 10, height: maxY - minY || 10 };
      }
      case 'ARROW': {
        const a = obj as ArrowObject;
        const minX = Math.min(a.x, a.x2);
        const maxX = Math.max(a.x, a.x2);
        const minY = Math.min(a.y, a.y2);
        const maxY = Math.max(a.y, a.y2);
        return { x: minX, y: minY, width: maxX - minX || 10, height: maxY - minY || 10 };
      }
      case 'POLYGON': {
        const pg = obj as PolygonObject;
        return { x: pg.x - pg.radius, y: pg.y - pg.radius, width: pg.radius * 2, height: pg.radius * 2 };
      }
      case 'TRIANGLE': {
        const tri = obj as TriangleObject;
        return { x: tri.x - tri.radius, y: tri.y - tri.radius, width: tri.radius * 2, height: tri.radius * 2 };
      }
      case 'STAR': {
        const star = obj as StarObject;
        return { x: star.x - star.outerRadius, y: star.y - star.outerRadius, width: star.outerRadius * 2, height: star.outerRadius * 2 };
      }
      case 'HEART': {
        const heart = obj as HeartObject;
        return { x: heart.x - heart.size, y: heart.y - heart.size * 0.3, width: heart.size * 2, height: heart.size * 1.3 };
      }
      case 'DIAMOND': {
        const diamond = obj as DiamondObject;
        return { x: diamond.x - diamond.width / 2, y: diamond.y - diamond.height / 2, width: diamond.width, height: diamond.height };
      }
      case 'BEZIER': {
        const bezier = obj as BezierObject;
        if (bezier.points.length === 0) return { x: 0, y: 0, width: 0, height: 0 };
        let minX = Infinity, minY = Infinity, maxX = -Infinity, maxY = -Infinity;
        bezier.points.forEach(pt => {
          minX = Math.min(minX, pt.x, pt.cp1x ?? pt.x, pt.cp2x ?? pt.x);
          minY = Math.min(minY, pt.y, pt.cp1y ?? pt.y, pt.cp2y ?? pt.y);
          maxX = Math.max(maxX, pt.x, pt.cp1x ?? pt.x, pt.cp2x ?? pt.x);
          maxY = Math.max(maxY, pt.y, pt.cp1y ?? pt.y, pt.cp2y ?? pt.y);
        });
        return { x: minX, y: minY, width: maxX - minX, height: maxY - minY };
      }
      case 'SMOOTH_CURVE': {
        const curve = obj as SmoothCurveObject;
        if (curve.points.length === 0) return { x: 0, y: 0, width: 0, height: 0 };
        const minX = Math.min(...curve.points.map(pt => pt.x));
        const maxX = Math.max(...curve.points.map(pt => pt.x));
        const minY = Math.min(...curve.points.map(pt => pt.y));
        const maxY = Math.max(...curve.points.map(pt => pt.y));
        return { x: minX, y: minY, width: maxX - minX, height: maxY - minY };
      }
      case 'GROUP': {
        const g = obj as GroupObject;
        // GROUP 的 x, y 是中心点，需要转换为左上角
        return { x: g.x - g.width / 2, y: g.y - g.height / 2, width: g.width, height: g.height };
      }
    }
    return { x: 0, y: 0, width: 0, height: 0 };
  }

  // 检查调整大小手柄
  private getResizeHandleAtPoint(obj: CanvasObject, x: number, y: number): string | null {
    const bounds = this.getObjectBounds(obj);
    const handleSize = 8;
    const centerX = bounds.x + bounds.width / 2;
    const centerY = bounds.y + bounds.height / 2;

    // 本地坐标系中的手柄位置
    const localHandles = [
      { name: 'nw', x: bounds.x, y: bounds.y },
      { name: 'ne', x: bounds.x + bounds.width, y: bounds.y },
      { name: 'sw', x: bounds.x, y: bounds.y + bounds.height },
      { name: 'se', x: bounds.x + bounds.width, y: bounds.y + bounds.height },
    ];

    // 如果对象有旋转或斜切，将本地手柄坐标转换为屏幕坐标
    const handles = localHandles.map(handle => {
      if (obj.rotation || obj.skewX || obj.skewY) {
        let dx = handle.x - centerX;
        let dy = handle.y - centerY;

        // 先应用斜切变换
        if (obj.skewX || obj.skewY) {
          const newDx = dx + (obj.skewX || 0) * dy;
          const newDy = dy + (obj.skewY || 0) * dx;
          dx = newDx;
          dy = newDy;
        }

        // 再应用旋转变换
        if (obj.rotation) {
          const cos = Math.cos(obj.rotation);
          const sin = Math.sin(obj.rotation);
          const rotatedDx = dx * cos - dy * sin;
          const rotatedDy = dx * sin + dy * cos;
          dx = rotatedDx;
          dy = rotatedDy;
        }

        return {
          name: handle.name,
          x: centerX + dx,
          y: centerY + dy
        };
      }
      return handle;
    });

    for (const handle of handles) {
      if (Math.abs(x - handle.x) <= handleSize && Math.abs(y - handle.y) <= handleSize) {
        return handle.name;
      }
    }
    return null;
  }

  // 获取旋转手柄位置（考虑对象旋转和斜切后的屏幕坐标）
  private getRotateHandlePosition(obj: CanvasObject): Point {
    const bounds = this.getObjectBounds(obj);
    const rotateHandleDistance = 30;
    const centerX = bounds.x + bounds.width / 2;
    const centerY = bounds.y + bounds.height / 2;

    // 旋转手柄在本地坐标系中的位置（在对象上方中心）
    let dx = 0; // centerX - centerX
    let dy = bounds.y - rotateHandleDistance - centerY;

    // 如果对象有旋转或斜切，将本地坐标转换为屏幕坐标
    if (obj.rotation || obj.skewX || obj.skewY) {
      // 先应用斜切变换
      if (obj.skewX || obj.skewY) {
        const newDx = dx + (obj.skewX || 0) * dy;
        const newDy = dy + (obj.skewY || 0) * dx;
        dx = newDx;
        dy = newDy;
      }

      // 再应用旋转变换
      if (obj.rotation) {
        const cos = Math.cos(obj.rotation);
        const sin = Math.sin(obj.rotation);
        const rotatedDx = dx * cos - dy * sin;
        const rotatedDy = dx * sin + dy * cos;
        dx = rotatedDx;
        dy = rotatedDy;
      }
    }

    return { x: centerX + dx, y: centerY + dy };
  }

  // 检测是否点击了旋转手柄
  private isPointOnRotateHandle(obj: CanvasObject, x: number, y: number): boolean {
    const handlePos = this.getRotateHandlePosition(obj);
    const handleRadius = 8; // 稍大一点的点击区域
    const dist = Math.sqrt(Math.pow(x - handlePos.x, 2) + Math.pow(y - handlePos.y, 2));
    return dist <= handleRadius;
  }

  // 碰撞检测
  private isHit(obj: CanvasObject, x: number, y: number): boolean {
    // 如果对象有旋转或斜切，需要将点击坐标反向变换到对象的本地坐标系
    let localX = x;
    let localY = y;
    const bounds = this.getObjectBounds(obj);
    const centerX = bounds.x + bounds.width / 2;
    const centerY = bounds.y + bounds.height / 2;

    if (obj.rotation || obj.skewX || obj.skewY) {
      // 先平移到中心
      let dx = x - centerX;
      let dy = y - centerY;

      // 反向旋转
      if (obj.rotation) {
        const cos = Math.cos(-obj.rotation);
        const sin = Math.sin(-obj.rotation);
        const newDx = dx * cos - dy * sin;
        const newDy = dx * sin + dy * cos;
        dx = newDx;
        dy = newDy;
      }

      // 反向斜切（逆矩阵）
      if (obj.skewX || obj.skewY) {
        const skewX = obj.skewX || 0;
        const skewY = obj.skewY || 0;
        const det = 1 - skewX * skewY;
        if (Math.abs(det) > 0.001) {
          const newDx = (dx - skewX * dy) / det;
          const newDy = (dy - skewY * dx) / det;
          dx = newDx;
          dy = newDy;
        }
      }

      localX = centerX + dx;
      localY = centerY + dy;
    }

    switch (obj.type) {
      case 'RECTANGLE': {
        const r = obj as RectObject;
        return localX >= r.x && localX <= r.x + r.width && localY >= r.y && localY <= r.y + r.height;
      }
      case 'CIRCLE': {
        const c = obj as CircleObject;
        const dist = Math.sqrt(Math.pow(localX - c.x, 2) + Math.pow(localY - c.y, 2));
        return dist <= c.radius;
      }
      case 'IMAGE': {
        const img = obj as ImageObject;
        return localX >= img.x && localX <= img.x + img.width && localY >= img.y && localY <= img.y + img.height;
      }
      case 'TEXT': {
        const t = obj as TextObject;
        const width = this.measureTextWidth(t.text, t.fontSize);
        // 文本现在是居中绘制的，x,y 是中心点
        const left = t.x - width / 2;
        const top = t.y - t.fontSize / 2;
        return localX >= left && localX <= left + width && localY >= top && localY <= top + t.fontSize;
      }
      case 'RICH_TEXT': {
        const rt = obj as RichTextObject;
        const width = this.measureRichTextWidth(rt);
        const maxFontSize = this.getRichTextMaxFontSize(rt);
        const height = maxFontSize * (rt.lineHeight || 1.2);

        // 根据 textAlign 计算边界
        let left: number, top: number;
        if (rt.textAlign === 'center') {
          left = rt.x - width / 2;
          top = rt.y - maxFontSize / 2;
        } else if (rt.textAlign === 'right') {
          left = rt.x - width;
          top = rt.y - maxFontSize;
        } else {
          left = rt.x;
          top = rt.y - maxFontSize;
        }
        return localX >= left && localX <= left + width && localY >= top && localY <= top + height;
      }
      case 'PATH': {
        const p = obj as PathObject;
        if (p.points.length === 0) return false;
        const minX = Math.min(...p.points.map(pt => pt.x));
        const maxX = Math.max(...p.points.map(pt => pt.x));
        const minY = Math.min(...p.points.map(pt => pt.y));
        const maxY = Math.max(...p.points.map(pt => pt.y));
        return localX >= minX && localX <= maxX && localY >= minY && localY <= maxY;
      }
      case 'LINE': {
        const l = obj as LineObject;
        // 检查点到线段的距离
        const dist = this.pointToLineDistance(localX, localY, l.x, l.y, l.x2, l.y2);
        return dist <= 10;
      }
      case 'ARROW': {
        const a = obj as ArrowObject;
        const dist = this.pointToLineDistance(localX, localY, a.x, a.y, a.x2, a.y2);
        return dist <= 10;
      }
      case 'POLYGON': {
        const pg = obj as PolygonObject;
        const dist = Math.sqrt(Math.pow(localX - pg.x, 2) + Math.pow(localY - pg.y, 2));
        return dist <= pg.radius;
      }
      case 'TRIANGLE': {
        const tri = obj as TriangleObject;
        const dist = Math.sqrt(Math.pow(localX - tri.x, 2) + Math.pow(localY - tri.y, 2));
        return dist <= tri.radius;
      }
      case 'STAR': {
        const star = obj as StarObject;
        const dist = Math.sqrt(Math.pow(localX - star.x, 2) + Math.pow(localY - star.y, 2));
        return dist <= star.outerRadius;
      }
      case 'HEART': {
        const heart = obj as HeartObject;
        const bounds = this.getObjectBounds(heart);
        return localX >= bounds.x && localX <= bounds.x + bounds.width && localY >= bounds.y && localY <= bounds.y + bounds.height;
      }
      case 'DIAMOND': {
        const diamond = obj as DiamondObject;
        const bounds = this.getObjectBounds(diamond);
        return localX >= bounds.x && localX <= bounds.x + bounds.width && localY >= bounds.y && localY <= bounds.y + bounds.height;
      }
      case 'BEZIER': {
        const bezier = obj as BezierObject;
        const bounds = this.getObjectBounds(bezier);
        // 简单的边界框检测
        return localX >= bounds.x && localX <= bounds.x + bounds.width && localY >= bounds.y && localY <= bounds.y + bounds.height;
      }
      case 'SMOOTH_CURVE': {
        const curve = obj as SmoothCurveObject;
        const bounds = this.getObjectBounds(curve);
        // 简单的边界框检测
        return localX >= bounds.x && localX <= bounds.x + bounds.width && localY >= bounds.y && localY <= bounds.y + bounds.height;
      }
      case 'GROUP': {
        const g = obj as GroupObject;
        // GROUP 的 x, y 是中心点，需要转换为左上角进行检测
        const left = g.x - g.width / 2;
        const top = g.y - g.height / 2;
        return localX >= left && localX <= left + g.width && localY >= top && localY <= top + g.height;
      }
    }
    return false;
  }

  // 计算点到线段的距离
  private pointToLineDistance(px: number, py: number, x1: number, y1: number, x2: number, y2: number): number {
    const A = px - x1;
    const B = py - y1;
    const C = x2 - x1;
    const D = y2 - y1;
    const dot = A * C + B * D;
    const lenSq = C * C + D * D;
    let param = -1;
    if (lenSq !== 0) param = dot / lenSq;
    let xx, yy;
    if (param < 0) {
      xx = x1;
      yy = y1;
    } else if (param > 1) {
      xx = x2;
      yy = y2;
    } else {
      xx = x1 + param * C;
      yy = y1 + param * D;
    }
    return Math.sqrt(Math.pow(px - xx, 2) + Math.pow(py - yy, 2));
  }

  // 保存历史
  private saveHistory(): void {
    this.history.push(JSON.parse(JSON.stringify(this.objects)));
    // 执行新操作时清空重做历史
    this.redoHistory = [];
    this.updateUI();
  }

  // 撤销
  private undo(): void {
    if (this.history.length === 0) return;
    // 保存当前状态到重做历史
    this.redoHistory.push(JSON.parse(JSON.stringify(this.objects)));
    const previousState = this.history.pop();
    if (previousState) {
      this.objects = previousState;
      this.selectedId = null;
      this.clearSelection();
      this.renderCanvas();
      this.updateUI();
      this.dispatchChangeEvent();
    }
  }

  // 重做
  private redo(): void {
    if (this.redoHistory.length === 0) return;
    // 保存当前状态到撤销历史
    this.history.push(JSON.parse(JSON.stringify(this.objects)));
    const nextState = this.redoHistory.pop();
    if (nextState) {
      this.objects = nextState;
      this.selectedId = null;
      this.clearSelection();
      this.renderCanvas();
      this.updateUI();
      this.dispatchChangeEvent();
    }
  }

  // ========== 图层管理功能 ==========

  // 切换图层面板
  private toggleLayerPanel(): void {
    this.layerPanelVisible = !this.layerPanelVisible;
    const layerPanel = this.shadow.querySelector('.layer-panel') as HTMLElement;
    if (layerPanel) {
      layerPanel.style.display = this.layerPanelVisible ? 'block' : 'none';
      if (this.layerPanelVisible) {
        this.renderLayerList();
      }
    }
  }

  // 渲染图层列表
  private renderLayerList(): void {
    const layerList = this.shadow.querySelector('.layer-panel-list') as HTMLElement;
    if (!layerList) return;

    // 从顶层到底层显示（数组倒序）
    const reversedObjects = [...this.objects].reverse();

    layerList.innerHTML = reversedObjects.map((obj) => {
      const isSelected = this.selectedId === obj.id || this.selectedIds.has(obj.id);
      const name = obj.type === 'TEXT' ? (obj as TextObject).text.substring(0, 10) : obj.type;
      const hidden = obj.visible === false;

      return `
        <div class="layer-item ${isSelected ? 'selected' : ''}" data-id="${obj.id}">
          <canvas class="layer-item-thumbnail" data-id="${obj.id}" width="40" height="40"></canvas>
          <span class="layer-item-name" style="${hidden ? 'opacity: 0.4;' : ''}">${name}</span>
          <div class="layer-item-actions">
            <button class="layer-item-btn layer-visibility-btn" data-id="${obj.id}" title="${hidden ? this.t('show') : this.t('hide')}">
              ${hidden ? '👁‍🗨' : '👁'}
            </button>
            <button class="layer-item-btn layer-up-btn" data-id="${obj.id}" title="${this.t('layerUp')}">↑</button>
            <button class="layer-item-btn layer-down-btn" data-id="${obj.id}" title="${this.t('layerDown')}">↓</button>
          </div>
        </div>
      `;
    }).join('');

    // 渲染缩略图
    layerList.querySelectorAll('.layer-item-thumbnail').forEach((canvas) => {
      const thumbCanvas = canvas as HTMLCanvasElement;
      const id = thumbCanvas.dataset.id;
      const obj = this.objects.find(o => o.id === id);
      if (obj) {
        this.renderThumbnail(thumbCanvas, obj);
      }
    });

    // 绑定事件
    layerList.querySelectorAll('.layer-item').forEach(item => {
      item.addEventListener('click', (e) => {
        const target = e.target as HTMLElement;
        if (target.classList.contains('layer-item-btn')) return;
        const id = (item as HTMLElement).dataset.id;
        if (id) {
          this.selectedId = id;
          this.clearSelection();
          this.renderCanvas();
          this.updateUI();
          this.renderLayerList();
        }
      });
    });

    layerList.querySelectorAll('.layer-visibility-btn').forEach(btn => {
      btn.addEventListener('click', (e) => {
        e.stopPropagation();
        const id = (btn as HTMLElement).dataset.id;
        if (id) {
          this.toggleLayerVisibility(id);
          this.renderLayerList();
        }
      });
    });

    layerList.querySelectorAll('.layer-up-btn').forEach(btn => {
      btn.addEventListener('click', (e) => {
        e.stopPropagation();
        const id = (btn as HTMLElement).dataset.id;
        if (id) {
          this.moveLayerUp(id);
          this.renderLayerList();
        }
      });
    });

    layerList.querySelectorAll('.layer-down-btn').forEach(btn => {
      btn.addEventListener('click', (e) => {
        e.stopPropagation();
        const id = (btn as HTMLElement).dataset.id;
        if (id) {
          this.moveLayerDown(id);
          this.renderLayerList();
        }
      });
    });
  }

  // 渲染单个对象的缩略图
  private renderThumbnail(canvas: HTMLCanvasElement, obj: CanvasObject): void {
    const ctx = canvas.getContext('2d');
    if (!ctx) return;

    const size = 40;
    ctx.clearRect(0, 0, size, size);
    ctx.fillStyle = '#f8fafc';
    ctx.fillRect(0, 0, size, size);

    // 获取对象边界
    const bounds = this.getObjectBounds(obj);
    const padding = 4;
    const availableSize = size - padding * 2;

    // 计算缩放比例
    const scaleX = availableSize / Math.max(bounds.width, 1);
    const scaleY = availableSize / Math.max(bounds.height, 1);
    const scale = Math.min(scaleX, scaleY, 1);

    // 居中偏移
    const offsetX = padding + (availableSize - bounds.width * scale) / 2 - bounds.x * scale;
    const offsetY = padding + (availableSize - bounds.height * scale) / 2 - bounds.y * scale;

    ctx.save();
    ctx.translate(offsetX, offsetY);
    ctx.scale(scale, scale);

    // 绘制对象（临时保存选中状态）
    const tempSelectedId = this.selectedId;
    this.selectedId = null;
    this.drawObject(ctx, { ...obj, visible: true });
    this.selectedId = tempSelectedId;

    ctx.restore();
  }

  // 上移图层
  private moveLayerUp(id: string): void {
    const index = this.objects.findIndex(o => o.id === id);
    if (index < this.objects.length - 1) {
      this.saveHistory();
      [this.objects[index], this.objects[index + 1]] = [this.objects[index + 1], this.objects[index]];
      this.renderCanvas();
      this.dispatchChangeEvent();
    }
  }

  // 下移图层
  private moveLayerDown(id: string): void {
    const index = this.objects.findIndex(o => o.id === id);
    if (index > 0) {
      this.saveHistory();
      [this.objects[index], this.objects[index - 1]] = [this.objects[index - 1], this.objects[index]];
      this.renderCanvas();
      this.dispatchChangeEvent();
    }
  }

  // 置顶图层
  private moveLayerToTop(id: string): void {
    const index = this.objects.findIndex(o => o.id === id);
    if (index >= 0 && index < this.objects.length - 1) {
      this.saveHistory();
      const obj = this.objects.splice(index, 1)[0];
      this.objects.push(obj);
      this.renderCanvas();
      this.dispatchChangeEvent();
    }
  }

  // 置底图层
  private moveLayerToBottom(id: string): void {
    const index = this.objects.findIndex(o => o.id === id);
    if (index > 0) {
      this.saveHistory();
      const obj = this.objects.splice(index, 1)[0];
      this.objects.unshift(obj);
      this.renderCanvas();
      this.dispatchChangeEvent();
    }
  }

  // 切换图层可见性
  private toggleLayerVisibility(id: string): void {
    const obj = this.objects.find(o => o.id === id);
    if (obj) {
      this.saveHistory();
      obj.visible = obj.visible === false ? true : false;
      this.renderCanvas();
      this.dispatchChangeEvent();
    }
  }

  // 切换图层锁定
  private toggleLayerLock(id: string): void {
    const obj = this.objects.find(o => o.id === id);
    if (obj) {
      this.saveHistory();
      obj.locked = !obj.locked;
      this.renderCanvas();
      this.dispatchChangeEvent();
    }
  }

  // ========== 组合/解组功能 ==========

  // 转换子对象坐标为相对坐标（组合时使用）
  private convertToRelativeCoords(obj: CanvasObject, offsetX: number, offsetY: number): CanvasObject {
    const result = { ...obj, x: obj.x - offsetX, y: obj.y - offsetY };
    // 处理 LINE 和 ARROW 的 x2, y2
    if (obj.type === 'LINE' || obj.type === 'ARROW') {
      const lineObj = result as LineObject | ArrowObject;
      lineObj.x2 = (obj as LineObject | ArrowObject).x2 - offsetX;
      lineObj.y2 = (obj as LineObject | ArrowObject).y2 - offsetY;
    }
    // 处理 PATH 的 points
    if (obj.type === 'PATH') {
      const pathObj = result as PathObject;
      pathObj.points = (obj as PathObject).points.map(pt => ({ x: pt.x - offsetX, y: pt.y - offsetY }));
    }
    return result;
  }

  // 转换子对象坐标为绝对坐标（解组时使用）
  private convertToAbsoluteCoords(obj: CanvasObject, offsetX: number, offsetY: number): CanvasObject {
    const result = { ...obj, x: obj.x + offsetX, y: obj.y + offsetY, id: this.generateId() };
    // 处理 LINE 和 ARROW 的 x2, y2
    if (obj.type === 'LINE' || obj.type === 'ARROW') {
      const lineObj = result as LineObject | ArrowObject;
      lineObj.x2 = (obj as LineObject | ArrowObject).x2 + offsetX;
      lineObj.y2 = (obj as LineObject | ArrowObject).y2 + offsetY;
    }
    // 处理 PATH 的 points
    if (obj.type === 'PATH') {
      const pathObj = result as PathObject;
      pathObj.points = (obj as PathObject).points.map(pt => ({ x: pt.x + offsetX, y: pt.y + offsetY }));
    }
    return result;
  }

  // 组合选中对象
  private groupSelected(): void {
    if (this.selectedIds.size < 2) return;

    this.saveHistory();
    const ids = Array.from(this.selectedIds);
    const children = this.objects.filter(o => ids.includes(o.id));
    if (children.length < 2) return;

    // 计算组合边界
    let minX = Infinity, minY = Infinity, maxX = -Infinity, maxY = -Infinity;
    children.forEach(obj => {
      const bounds = this.getObjectBounds(obj);
      minX = Math.min(minX, bounds.x);
      minY = Math.min(minY, bounds.y);
      maxX = Math.max(maxX, bounds.x + bounds.width);
      maxY = Math.max(maxY, bounds.y + bounds.height);
    });

    // 创建组合对象，使用相对坐标
    const groupObj: GroupObject = {
      id: this.generateId(),
      type: 'GROUP',
      x: minX,
      y: minY,
      width: maxX - minX,
      height: maxY - minY,
      color: this.color,
      lineWidth: this.lineWidth,
      children: children.map(c => this.convertToRelativeCoords(c, minX, minY)),
    };

    // 移除原对象，添加组合
    this.objects = this.objects.filter(o => !ids.includes(o.id));
    this.objects.push(groupObj);
    this.clearSelection();
    this.selectedId = groupObj.id;
    this.renderCanvas();
    this.updateUI();
    this.dispatchChangeEvent();
  }

  // 解组选中对象
  private ungroupSelected(): void {
    if (!this.selectedId) return;
    const obj = this.objects.find(o => o.id === this.selectedId);
    if (!obj || obj.type !== 'GROUP') return;

    this.saveHistory();
    const groupObj = obj as GroupObject;
    // 使用绝对坐标还原子对象
    const children = groupObj.children.map(c => this.convertToAbsoluteCoords(c, groupObj.x, groupObj.y));

    // 移除组合，添加子对象
    this.objects = this.objects.filter(o => o.id !== this.selectedId);
    this.objects.push(...children);
    this.selectedId = null;
    this.clearSelection();
    children.forEach(c => this.selectedIds.add(c.id));
    this.renderCanvas();
    this.updateUI();
    this.dispatchChangeEvent();
  }

  // ========== 对齐/分布功能 ==========

  // 获取选中对象列表
  private getSelectedObjects(): CanvasObject[] {
    if (this.selectedIds.size > 0) {
      return this.objects.filter(o => this.selectedIds.has(o.id));
    } else if (this.selectedId) {
      const obj = this.objects.find(o => o.id === this.selectedId);
      return obj ? [obj] : [];
    }
    return [];
  }

  // 获取多选对象的整体边界
  private getMultiSelectionBounds(): { x: number; y: number; width: number; height: number } | null {
    if (this.selectedIds.size < 2) return null;

    let minX = Infinity, minY = Infinity, maxX = -Infinity, maxY = -Infinity;
    this.selectedIds.forEach(id => {
      const obj = this.objects.find(o => o.id === id);
      if (obj) {
        const bounds = this.getObjectBounds(obj);
        minX = Math.min(minX, bounds.x);
        minY = Math.min(minY, bounds.y);
        maxX = Math.max(maxX, bounds.x + bounds.width);
        maxY = Math.max(maxY, bounds.y + bounds.height);
      }
    });

    if (minX === Infinity) return null;
    return { x: minX, y: minY, width: maxX - minX, height: maxY - minY };
  }

  // 获取多选变换原点（自定义或默认中心点）
  private getMultiTransformOrigin(): Point | null {
    if (this.selectedIds.size < 2) return null;

    if (this.transformOrigin) {
      return this.transformOrigin;
    }

    const bounds = this.getMultiSelectionBounds();
    if (!bounds) return null;

    return {
      x: bounds.x + bounds.width / 2,
      y: bounds.y + bounds.height / 2
    };
  }

  // 检测是否点击了变换原点
  private isPointOnTransformOrigin(x: number, y: number): boolean {
    const origin = this.getMultiTransformOrigin();
    if (!origin) return false;

    const handleRadius = 10;
    const dist = Math.sqrt(Math.pow(x - origin.x, 2) + Math.pow(y - origin.y, 2));
    return dist <= handleRadius;
  }

  // 左对齐
  private alignLeft(): void {
    const selected = this.getSelectedObjects();
    if (selected.length < 2) return;
    this.saveHistory();
    const minX = Math.min(...selected.map(o => this.getObjectBounds(o).x));
    selected.forEach(obj => {
      const bounds = this.getObjectBounds(obj);
      obj.x = obj.x + (minX - bounds.x);
    });
    this.renderCanvas();
    this.dispatchChangeEvent();
  }

  // 水平居中对齐
  private alignCenterH(): void {
    const selected = this.getSelectedObjects();
    if (selected.length < 2) return;
    this.saveHistory();
    const bounds = selected.map(o => this.getObjectBounds(o));
    const minX = Math.min(...bounds.map(b => b.x));
    const maxX = Math.max(...bounds.map(b => b.x + b.width));
    const centerX = (minX + maxX) / 2;
    selected.forEach((obj, i) => {
      const b = bounds[i];
      obj.x = obj.x + (centerX - (b.x + b.width / 2));
    });
    this.renderCanvas();
    this.dispatchChangeEvent();
  }

  // 右对齐
  private alignRight(): void {
    const selected = this.getSelectedObjects();
    if (selected.length < 2) return;
    this.saveHistory();
    const bounds = selected.map(o => this.getObjectBounds(o));
    const maxX = Math.max(...bounds.map(b => b.x + b.width));
    selected.forEach((obj, i) => {
      const b = bounds[i];
      obj.x = obj.x + (maxX - (b.x + b.width));
    });
    this.renderCanvas();
    this.dispatchChangeEvent();
  }

  // 删除选中对象
  private deleteSelected(): void {
    if (this.selectedId) {
      this.saveHistory();
      this.objects = this.objects.filter(o => o.id !== this.selectedId);
      this.selectedId = null;
      this.renderCanvas();
      this.updateUI();
      this.dispatchChangeEvent();
    } else if (this.selectedIds.size > 0) {
      this.saveHistory();
      this.objects = this.objects.filter(o => !this.selectedIds.has(o.id));
      this.clearSelection();
      this.renderCanvas();
      this.updateUI();
      this.dispatchChangeEvent();
    }
  }

  // 复制选中对象
  private copySelected(): void {
    if (this.selectedId) {
      const selectedObj = this.objects.find(o => o.id === this.selectedId);
      if (selectedObj) {
        this.clipboard = JSON.parse(JSON.stringify(selectedObj));
      }
    }
  }

  // 粘贴对象
  private pasteObject(): void {
    if (this.clipboard) {
      this.saveHistory();
      const newObj = {
        ...JSON.parse(JSON.stringify(this.clipboard)),
        id: this.generateId(),
        x: this.clipboard.x + 20,
        y: this.clipboard.y + 20
      };
      if (newObj.type === 'PATH' && newObj.points) {
        newObj.points = newObj.points.map((pt: Point) => ({
          x: pt.x + 20,
          y: pt.y + 20
        }));
      }
      this.objects.push(newObj);
      this.selectedId = newObj.id;
      this.clipboard = newObj;
      this.renderCanvas();
      this.updateUI();
      this.dispatchChangeEvent();
    }
  }

  // 全选
  private selectAll(): void {
    this.selectedId = null;
    this.clearSelection();
    this.objects.forEach(obj => {
      if (obj.visible !== false) {
        this.selectedIds.add(obj.id);
      }
    });
    this.renderCanvas();
    this.updateUI();
  }

  // 清除选择并重置变换原点
  private clearSelection(): void {
    this.selectedIds.clear();
    this.transformOrigin = null;
  }

  // 更新对象颜色（支持 GROUP 对象的子对象颜色更新）
  private updateObjectColor(obj: CanvasObject, newColor: string): void {
    obj.color = newColor;

    // 如果是 GROUP 对象，更新子对象的颜色
    if (obj.type === 'GROUP') {
      const groupObj = obj as GroupObject;
      groupObj.children.forEach(child => {
        // 更新子对象的边框颜色
        child.color = newColor;

        // 如果子对象有填充色，也更新填充色
        if ('fillColor' in child && child.fillColor) {
          (child as any).fillColor = newColor;
        }

        // 如果是文本对象，不更新颜色（保持文本颜色独立）
        // 只更新形状的颜色
        if (child.type !== 'TEXT' && child.type !== 'RICH_TEXT') {
          child.color = newColor;
          if ('fillColor' in child) {
            (child as any).fillColor = newColor;
          }
        }
      });
    }

    // 如果对象有填充色属性，也更新
    if ('fillColor' in obj && (obj as any).fillColor) {
      (obj as any).fillColor = newColor;
    }
  }

  // 派发变化事件
  private dispatchChangeEvent(): void {
    this.dispatchEvent(new CustomEvent('editor-change', {
      bubbles: true,
      composed: true,
      detail: { objects: this.objects }
    }));
    // 如果图层面板可见，刷新列表
    if (this.layerPanelVisible) {
      this.renderLayerList();
    }
  }

  // 键盘事件处理
  private handleKeyDown(e: KeyboardEvent): void {
    // 空格键按下：启用平移模式
    if (e.code === 'Space' && !this.isTextInputVisible) {
      e.preventDefault();
      this.isSpacePressed = true;
      this.canvas.style.cursor = 'grab';
      return;
    }

    if (this.isTextInputVisible) return;

    // 如果焦点在输入框或文本域中，不处理快捷键
    const activeElement = this.shadow.activeElement || document.activeElement;
    if (activeElement && (activeElement.tagName === 'INPUT' || activeElement.tagName === 'TEXTAREA')) {
      return;
    }

    // Ctrl+Shift+Z 或 Ctrl+Y: 重做
    if ((e.ctrlKey || e.metaKey) && (e.key === 'y' || (e.shiftKey && e.key === 'z'))) {
      e.preventDefault();
      this.redo();
      return;
    }

    // Ctrl+Z: 撤销
    if ((e.ctrlKey || e.metaKey) && e.key === 'z' && !e.shiftKey) {
      e.preventDefault();
      this.undo();
      return;
    }

    // Ctrl+G: 组合
    if ((e.ctrlKey || e.metaKey) && e.key === 'g' && !e.shiftKey) {
      e.preventDefault();
      this.groupSelected();
      return;
    }

    // Ctrl+Shift+U: 解组
    if ((e.ctrlKey || e.metaKey) && e.shiftKey && e.key.toLowerCase() === 'u') {
      e.preventDefault();
      this.ungroupSelected();
      return;
    }

    // Ctrl+A: 全选
    if ((e.ctrlKey || e.metaKey) && e.key === 'a') {
      e.preventDefault();
      this.selectAll();
      return;
    }

    // Ctrl+C: 复制
    if ((e.ctrlKey || e.metaKey) && e.key === 'c') {
      if (this.selectedId) {
        e.preventDefault();
        this.copySelected();
      }
      return;
    }

    // Ctrl+V: 粘贴
    if ((e.ctrlKey || e.metaKey) && e.key === 'v') {
      if (this.clipboard) {
        e.preventDefault();
        this.pasteObject();
      }
      return;
    }

    // Enter: 完成平滑曲线绘制
    if (e.key === 'Enter' && this.isSmoothCurveDrawing) {
      e.preventDefault();
      this.finishSmoothCurve();
      return;
    }

    // Escape: 取消平滑曲线绘制
    if (e.key === 'Escape' && this.isSmoothCurveDrawing) {
      e.preventDefault();
      this.smoothCurvePoints = [];
      this.isSmoothCurveDrawing = false;
      this.renderCanvas();
      return;
    }

    // Delete/Backspace: 删除
    if ((e.key === 'Delete' || e.key === 'Backspace') && (this.selectedId || this.selectedIds.size > 0)) {
      e.preventDefault();
      this.deleteSelected();
      return;
    }

    // 快捷键切换工具
    if (!e.ctrlKey && !e.metaKey) {
      switch (e.key.toLowerCase()) {
        case 'v':
          this.setTool('SELECT');
          break;
        case 'p':
        case 'b':
          this.setTool('PENCIL');
          break;
        case 'r':
          this.setTool('RECTANGLE');
          break;
        case 'o':
          this.setTool('CIRCLE');
          break;
        case 'l':
          this.setTool('LINE');
          break;
        case 'a':
          this.setTool('ARROW');
          break;
        case 't':
          this.setTool('TEXT');
          break;
        case 'escape':
          // 如果正在绘制贝塞尔曲线，取消绘制
          if (this.tool === 'BEZIER' && this.bezierPoints.length > 0) {
            this.bezierPoints = [];
            this.isBezierDrawing = false;
            this.bezierTempPoint = null;
            this.renderCanvas();
            break;
          }
          this.selectedId = null;
          this.clearSelection();
          this.hideTextInput();
          this.renderCanvas();
          this.updateUI();
          break;
        case 'enter':
          // 如果正在绘制贝塞尔曲线，完成路径（不闭合）
          if (this.tool === 'BEZIER' && this.bezierPoints.length >= 2) {
            this.finishBezierPath(false);
          }
          break;
      }
    }
  }

  // 键盘释放事件处理
  private handleKeyUp(e: KeyboardEvent): void {
    // 空格键释放：退出平移模式
    if (e.code === 'Space') {
      this.isSpacePressed = false;
      this.isPanning = false;
      this.canvas.style.cursor = this.tool === 'SELECT' ? 'default' : 'crosshair';
    }
  }

  // 滚轮缩放
  private handleWheel(e: WheelEvent): void {
    e.preventDefault();
    const rect = this.canvas.getBoundingClientRect();
    const mouseX = e.clientX - rect.left;
    const mouseY = e.clientY - rect.top;

    const delta = e.deltaY > 0 ? 0.9 : 1.1;
    const newScale = this.scale * delta;

    this.zoomAtPoint(newScale, mouseX, mouseY);
  }

  // 以指定点为中心缩放
  private zoomAtPoint(newScale: number, centerX: number, centerY: number): void {
    const clampedScale = Math.min(Math.max(newScale, 0.2), 5);

    const mouseXBeforeZoom = (centerX - this.panOffset.x) / this.scale;
    const mouseYBeforeZoom = (centerY - this.panOffset.y) / this.scale;

    const newPanOffsetX = centerX - mouseXBeforeZoom * clampedScale;
    const newPanOffsetY = centerY - mouseYBeforeZoom * clampedScale;

    this.scale = clampedScale;
    this.panOffset = { x: newPanOffsetX, y: newPanOffsetY };

    this.renderCanvas();
    this.updateZoomDisplay();
  }

  // 放大
  private zoomIn(): void {
    const centerX = this.canvas.width / 2;
    const centerY = this.canvas.height / 2;
    this.zoomAtPoint(this.scale * 1.2, centerX, centerY);
  }

  // 缩小
  private zoomOut(): void {
    const centerX = this.canvas.width / 2;
    const centerY = this.canvas.height / 2;
    this.zoomAtPoint(this.scale / 1.2, centerX, centerY);
  }

  // 重置缩放
  private resetZoom(): void {
    this.scale = 1;
    this.panOffset = { x: 0, y: 0 };
    this.renderCanvas();
    this.updateZoomDisplay();
  }

  // 更新缩放显示
  private updateZoomDisplay(): void {
    const zoomText = this.shadow.querySelector('.zoom-text');
    if (zoomText) {
      zoomText.textContent = `${Math.round(this.scale * 100)}%`;
    }
  }

  // 设置工具
  private setTool(tool: ToolType): void {
    // 如果正在绘制贝塞尔曲线且切换到其他工具，完成当前路径
    if (this.tool === 'BEZIER' && tool !== 'BEZIER' && this.bezierPoints.length >= 2) {
      this.finishBezierPath(false);
    } else if (this.tool === 'BEZIER' && tool !== 'BEZIER') {
      // 清理未完成的贝塞尔状态
      this.bezierPoints = [];
      this.isBezierDrawing = false;
      this.bezierTempPoint = null;
    }

    // 切换工具时，如果正在绘制平滑曲线，完成或清理
    if (this.tool === 'SMOOTH_CURVE' && tool !== 'SMOOTH_CURVE' && this.smoothCurvePoints.length >= 2) {
      this.finishSmoothCurve();
    } else if (this.tool === 'SMOOTH_CURVE' && tool !== 'SMOOTH_CURVE') {
      // 清理未完成的平滑曲线状态
      this.smoothCurvePoints = [];
      this.isSmoothCurveDrawing = false;
    }

    this.tool = tool;
    this.updateToolButtons();
    this.renderCanvas();
  }

  // 更新工具按钮状态
  private updateToolButtons(): void {
    const buttons = this.shadow.querySelectorAll('.tool-btn');
    buttons.forEach(btn => {
      const btnTool = btn.getAttribute('data-tool');
      if (btnTool === this.tool) {
        btn.classList.add('active');
      } else {
        btn.classList.remove('active');
      }
    });

    // 更新形状工具组的图标和选中状态
    const shapeTools = ['RECTANGLE', 'CIRCLE', 'TRIANGLE', 'STAR', 'HEART', 'DIAMOND', 'BEZIER', 'SMOOTH_CURVE', 'LINE', 'ARROW', 'DOUBLE_ARROW', 'POLYGON'];
    const shapesGroup = this.shadow.querySelector('.tool-group[data-group="shapes"]');
    if (shapesGroup) {
      const groupBtn = shapesGroup.querySelector('.tool-group-btn');
      if (groupBtn) {
        const isShapeTool = shapeTools.includes(this.tool);
        if (isShapeTool) {
          // 更新图标为当前选择的形状
          const svgIcon = groupBtn.querySelector('.icon');
          if (svgIcon) {
            svgIcon.innerHTML = this.getShapeIconPath(this.tool);
          }
          groupBtn.classList.add('active');
        } else {
          groupBtn.classList.remove('active');
        }
      }
    }

    // 更新文本工具组的图标和选中状态
    const textTools = ['TEXT', 'RICH_TEXT'];
    const textGroup = this.shadow.querySelector('.tool-group[data-group="text"]');
    if (textGroup) {
      const groupBtn = textGroup.querySelector('.tool-group-btn');
      if (groupBtn) {
        const isTextTool = textTools.includes(this.tool);
        if (isTextTool) {
          // 更新图标为当前选择的文本类型
          const svgIcon = groupBtn.querySelector('.icon');
          if (svgIcon) {
            svgIcon.innerHTML = this.getTextIconPath(this.tool);
          }
          groupBtn.classList.add('active');
        } else {
          groupBtn.classList.remove('active');
        }
      }
    }

    // 更新填充模式按钮的选中状态
    this.shadow.querySelectorAll('.fill-mode-btn').forEach(btn => {
      const mode = btn.getAttribute('data-fill-mode');
      if (mode === this.fillMode) {
        btn.classList.add('active');
      } else {
        btn.classList.remove('active');
      }
    });

    // 更新线条样式按钮的选中状态
    this.shadow.querySelectorAll('.line-style-btn').forEach(btn => {
      const style = btn.getAttribute('data-line-style');
      if (style === this.lineStyle) {
        btn.classList.add('active');
      } else {
        btn.classList.remove('active');
      }
    });
  }

  // 获取文本工具的图标路径
  private getTextIconPath(tool: ToolType): string {
    switch (tool) {
      case 'TEXT':
        return `<polyline points="4 7 4 4 20 4 20 7"/>
                <line x1="9" y1="20" x2="15" y2="20"/>
                <line x1="12" y1="4" x2="12" y2="20"/>`;
      case 'RICH_TEXT':
        return `<path d="M4 7V4h16v3"/>
                <path d="M9 20h6"/>
                <path d="M12 4v16"/>
                <path d="M7 12h4" stroke-width="3"/>`;
      default:
        return `<polyline points="4 7 4 4 20 4 20 7"/>
                <line x1="9" y1="20" x2="15" y2="20"/>
                <line x1="12" y1="4" x2="12" y2="20"/>`;
    }
  }

  // 获取形状工具的图标路径
  private getShapeIconPath(tool: ToolType): string {
    switch (tool) {
      case 'RECTANGLE':
        return '<rect x="3" y="3" width="18" height="18" rx="2"/>';
      case 'CIRCLE':
        return '<circle cx="12" cy="12" r="10"/>';
      case 'TRIANGLE':
        return '<polygon points="12 3 22 21 2 21"/>';
      case 'STAR':
        return '<polygon points="12 2 15.09 8.26 22 9.27 17 14.14 18.18 21.02 12 17.77 5.82 21.02 7 14.14 2 9.27 8.91 8.26 12 2"/>';
      case 'HEART':
        return '<path d="M20.84 4.61a5.5 5.5 0 0 0-7.78 0L12 5.67l-1.06-1.06a5.5 5.5 0 0 0-7.78 7.78l1.06 1.06L12 21.23l7.78-7.78 1.06-1.06a5.5 5.5 0 0 0 0-7.78z"/>';
      case 'DIAMOND':
        return '<polygon points="12 2 22 12 12 22 2 12"/>';
      case 'BEZIER':
        return '<path d="M3 17c3-6 9-6 12 0s9 6 12 0"/><circle cx="3" cy="17" r="2"/><circle cx="21" cy="17" r="2"/>';
      case 'SMOOTH_CURVE':
        return '<path d="M3 20c3-6 6-9 9-9s6 3 9 9" fill="none" stroke="currentColor" stroke-width="2"/><circle cx="3" cy="20" r="1.5"/><circle cx="12" cy="11" r="1.5"/><circle cx="21" cy="20" r="1.5"/>';
      case 'LINE':
        return '<line x1="5" y1="19" x2="19" y2="5"/>';
      case 'ARROW':
        return '<line x1="5" y1="19" x2="19" y2="5"/><polyline points="9 5 19 5 19 15"/>';
      case 'DOUBLE_ARROW':
        return '<line x1="5" y1="19" x2="19" y2="5"/><polyline points="9 5 19 5 19 15"/><polyline points="15 19 5 19 5 9"/>';
      case 'POLYGON':
        return '<polygon points="12 2 22 8.5 22 15.5 12 22 2 15.5 2 8.5 12 2"/>';
      default:
        return '<rect x="3" y="3" width="18" height="18" rx="2"/>';
    }
  }

  // 隐藏文本输入
  private hideTextInput(): void {
    this.isTextInputVisible = false;
    if (this.textInputContainer) {
      this.textInputContainer.style.display = 'none';
    }
    this.editingTextId = null;
  }

  // 显示文本输入
  private showTextInput(screenX: number, screenY: number, text: string = ''): void {
    this.isTextInputVisible = true;
    this.textInputScreenPos = { x: screenX, y: screenY };

    if (this.textInputContainer && this.textInput) {
      this.textInputContainer.style.display = 'block';
      this.textInputContainer.style.left = `${screenX}px`;
      this.textInputContainer.style.top = `${screenY - 30}px`;
      this.textInput.value = text;
      this.textInput.style.color = this.color;
      setTimeout(() => {
        this.textInput.focus();
        if (text) this.textInput.select();
      }, 0);
    }
  }

  // 提交文本
  private submitText(): void {
    const value = this.textInput?.value?.trim();
    if (value) {
      if (this.editingTextId) {
        const existingObj = this.objects.find(o => o.id === this.editingTextId) as TextObject | undefined;
        if (existingObj && existingObj.text !== value) {
          this.saveHistory();
          existingObj.text = value;
        }
        this.selectedId = this.editingTextId;
      } else {
        this.saveHistory();
        const newObj: TextObject = {
          id: this.generateId(),
          type: 'TEXT',
          x: this.textInputPos.x,
          y: this.textInputPos.y,
          text: value,
          fontSize: 24,
          color: this.color,
          lineWidth: this.lineWidth
        };
        this.objects.push(newObj);
        this.selectedId = newObj.id;
      }
      this.dispatchChangeEvent();
    }
    this.hideTextInput();
    this.setTool('SELECT');
    this.renderCanvas();
    this.updateUI();
  }

  // 画布鼠标按下
  private handleCanvasPointerDown(e: MouseEvent | TouchEvent): void {
    const { x, y } = this.getMousePos(e);
    const screenPos = this.getScreenPos(e);
    this.dragStart = { x, y };
    this.isDragging = true;

    // 如果文本输入可见且不是文本工具，先保存文本
    if (this.isTextInputVisible && this.tool !== 'TEXT') {
      this.submitText();
    }

    // 空格键按下时，开始平移画布
    if (this.isSpacePressed) {
      this.isPanning = true;
      this.panStart = screenPos;
      this.canvas.style.cursor = 'grabbing';
      return;
    }

    if (this.tool === 'SELECT') {
      // Ctrl 和 Shift 都支持多选
      const isMultiSelect = (e as MouseEvent).shiftKey || (e as MouseEvent).ctrlKey || (e as MouseEvent).metaKey;

      // 检查是否点击旋转手柄
      if (this.selectedId && !isMultiSelect) {
        const selectedObj = this.objects.find(o => o.id === this.selectedId);
        if (selectedObj && this.isPointOnRotateHandle(selectedObj, x, y)) {
          this.saveHistory();
          this.isRotating = true;
          const bounds = this.getObjectBounds(selectedObj);
          const centerX = bounds.x + bounds.width / 2;
          const centerY = bounds.y + bounds.height / 2;
          this.rotateStartAngle = Math.atan2(y - centerY, x - centerX);
          this.rotateObjectStartRotation = selectedObj.rotation || 0;
          return;
        }
      }

      // 检查是否点击斜切手柄
      if (this.selectedId && !isMultiSelect) {
        const selectedObj = this.objects.find(o => o.id === this.selectedId);
        if (selectedObj) {
          const skewHandle = this.getSkewHandleAtPoint(selectedObj, x, y);
          if (skewHandle) {
            this.saveHistory();
            this.isSkewing = true;
            this.skewHandle = skewHandle;
            this.skewStartPos = { x, y };
            this.skewObjectStartSkewX = selectedObj.skewX || 0;
            this.skewObjectStartSkewY = selectedObj.skewY || 0;
            return;
          }
        }
      }

      // 检查是否点击调整大小手柄
      if (this.selectedId && !isMultiSelect) {
        const selectedObj = this.objects.find(o => o.id === this.selectedId);
        if (selectedObj) {
          const handle = this.getResizeHandleAtPoint(selectedObj, x, y);
          if (handle) {
            this.saveHistory();
            this.isResizing = true;
            this.resizeHandle = handle;
            this.resizeStartBounds = this.getObjectBounds(selectedObj);
            this.resizeOriginalObject = JSON.parse(JSON.stringify(selectedObj));
            return;
          }
        }
      }

      // 检查是否点击了多选变换原点
      if (this.selectedIds.size >= 2 && this.isPointOnTransformOrigin(x, y)) {
        this.isDraggingOrigin = true;
        return;
      }

      // 检查是否点击了多选边界外围（用于多选旋转）
      if (this.selectedIds.size >= 2 && !isMultiSelect) {
        const multiBounds = this.getMultiSelectionBounds();
        if (multiBounds) {
          const origin = this.getMultiTransformOrigin();
          if (origin) {
            // 检查是否在边界框外围但在旋转区域内
            const expandedBounds = {
              x: multiBounds.x - 20,
              y: multiBounds.y - 40, // 上方留更多空间给旋转手柄
              width: multiBounds.width + 40,
              height: multiBounds.height + 60
            };
            const isInExpandedBounds = x >= expandedBounds.x && x <= expandedBounds.x + expandedBounds.width &&
                                       y >= expandedBounds.y && y <= expandedBounds.y + expandedBounds.height;
            const isInInnerBounds = x >= multiBounds.x - 5 && x <= multiBounds.x + multiBounds.width + 5 &&
                                    y >= multiBounds.y - 5 && y <= multiBounds.y + multiBounds.height + 5;

            // 点击在外围区域（边界框和扩展区域之间）
            if (isInExpandedBounds && !isInInnerBounds) {
              this.saveHistory();
              this.isMultiRotating = true;
              this.multiRotateStartAngle = Math.atan2(y - origin.y, x - origin.x);
              // 保存所有选中对象的初始状态
              this.multiRotateObjectsStart.clear();
              this.selectedIds.forEach(id => {
                const obj = this.objects.find(o => o.id === id);
                if (obj) {
                  this.multiRotateObjectsStart.set(id, {
                    x: obj.x,
                    y: obj.y,
                    rotation: obj.rotation || 0
                  });
                }
              });
              return;
            }
          }
        }
      }

      // 查找点击的对象（只检查可见对象）
      const clickedObject = [...this.objects].reverse().find(obj => obj.visible !== false && this.isHit(obj, x, y));

      if (clickedObject) {
        if (isMultiSelect) {
          // Ctrl/Shift+点击：多选模式
          if (this.selectedIds.has(clickedObject.id)) {
            // 如果已选中，取消选中
            this.selectedIds.delete(clickedObject.id);
            if (this.selectedId === clickedObject.id) {
              this.selectedId = null;
            }
          } else {
            // 添加到多选
            if (this.selectedId && !this.selectedIds.has(this.selectedId)) {
              this.selectedIds.add(this.selectedId);
            }
            this.selectedIds.add(clickedObject.id);
            this.selectedId = null;
          }
        } else if (this.selectedIds.size > 0 && this.selectedIds.has(clickedObject.id)) {
          // 点击已多选的对象：开始多选拖动
          this.isMultiDragging = true;
          this.multiDragStart = { x, y };
          this.saveHistory();
        } else {
          // 普通点击：单选
          this.selectedId = clickedObject.id;
          this.clearSelection();
          this.dragOffset = { x: x - clickedObject.x, y: y - clickedObject.y };
          this.saveHistory();
        }
        this.updateUI();
      } else {
        // 空白区域点击
        if (isMultiSelect) {
          // Ctrl/Shift + 空白区域拖拽：平移画布
          this.isPanning = true;
          this.panStart = screenPos;
          this.canvas.style.cursor = 'grabbing';
        } else {
          // 开始框选
          this.isSelecting = true;
          this.selectionRect = { x, y, width: 0, height: 0 };
          this.selectedId = null;
          this.clearSelection();
        }
        this.updateUI();
      }
    } else if (this.tool === 'TEXT') {
      // 显示文本输入
      this.textInputPos = { x, y };
      this.showTextInput(screenPos.x, screenPos.y);
      this.isDragging = false;
    } else if (this.tool === 'RICH_TEXT') {
      // 显示富文本编辑器
      this.showRichTextEditor(x, y);
      this.isDragging = false;
    } else {
      // 开始绘制图形
      this.saveHistory();
      // 立即隐藏空画布提示
      this.hideEmptyHint();
      const id = this.generateId();
      if (this.tool === 'RECTANGLE') {
        this.currentObject = { id, type: 'RECTANGLE', x, y, width: 0, height: 0, color: this.color, lineWidth: this.lineWidth, lineStyle: this.lineStyle, fillMode: this.fillMode } as RectObject;
      } else if (this.tool === 'CIRCLE') {
        this.currentObject = { id, type: 'CIRCLE', x, y, radius: 0, color: this.color, lineWidth: this.lineWidth, lineStyle: this.lineStyle, fillMode: this.fillMode } as CircleObject;
      } else if (this.tool === 'PENCIL') {
        this.currentObject = { id, type: 'PATH', x, y, points: [{ x, y }], color: this.color, lineWidth: this.lineWidth };
      } else if (this.tool === 'LINE') {
        this.currentObject = { id, type: 'LINE', x, y, x2: x, y2: y, color: this.color, lineWidth: this.lineWidth, lineStyle: this.lineStyle } as LineObject;
      } else if (this.tool === 'ARROW') {
        this.currentObject = { id, type: 'ARROW', x, y, x2: x, y2: y, color: this.color, lineWidth: this.lineWidth, lineStyle: this.lineStyle, arrowType: 'single' } as ArrowObject;
      } else if (this.tool === 'DOUBLE_ARROW') {
        this.currentObject = { id, type: 'ARROW', x, y, x2: x, y2: y, color: this.color, lineWidth: this.lineWidth, lineStyle: this.lineStyle, arrowType: 'double' } as ArrowObject;
      } else if (this.tool === 'POLYGON') {
        // 默认创建六边形
        this.currentObject = { id, type: 'POLYGON', x, y, radius: 0, sides: 6, color: this.color, lineWidth: this.lineWidth, lineStyle: this.lineStyle, fillMode: this.fillMode } as PolygonObject;
      } else if (this.tool === 'TRIANGLE') {
        this.currentObject = { id, type: 'TRIANGLE', x, y, radius: 0, color: this.color, lineWidth: this.lineWidth, lineStyle: this.lineStyle, fillMode: this.fillMode } as TriangleObject;
      } else if (this.tool === 'STAR') {
        this.currentObject = { id, type: 'STAR', x, y, points: 5, outerRadius: 0, innerRadius: 0, color: this.color, lineWidth: this.lineWidth, lineStyle: this.lineStyle, fillMode: this.fillMode } as StarObject;
      } else if (this.tool === 'HEART') {
        this.currentObject = { id, type: 'HEART', x, y, size: 0, color: this.color, lineWidth: this.lineWidth, lineStyle: this.lineStyle, fillMode: this.fillMode } as HeartObject;
      } else if (this.tool === 'DIAMOND') {
        this.currentObject = { id, type: 'DIAMOND', x, y, width: 0, height: 0, color: this.color, lineWidth: this.lineWidth, lineStyle: this.lineStyle, fillMode: this.fillMode } as DiamondObject;
      } else if (this.tool === 'BEZIER') {
        // 贝塞尔曲线工具特殊处理
        this.handleBezierClick(x, y);
        return;
      } else if (this.tool === 'SMOOTH_CURVE') {
        // 平滑曲线工具特殊处理
        this.handleSmoothCurveClick(x, y);
        return;
      }
    }

    this.renderCanvas();
  }

  // 处理平滑曲线点击
  private handleSmoothCurveClick(x: number, y: number): void {
    const currentTime = Date.now();
    const isDoubleClick = currentTime - this.lastClickTime < 300;
    this.lastClickTime = currentTime;

    // 双击完成曲线
    if (isDoubleClick && this.smoothCurvePoints.length >= 2) {
      this.finishSmoothCurve();
      return;
    }

    // 添加新点
    this.smoothCurvePoints.push({ x, y });
    this.isSmoothCurveDrawing = true;
    // 立即隐藏空画布提示
    this.hideEmptyHint();
    this.renderCanvas();
  }

  // 完成平滑曲线绘制
  private finishSmoothCurve(): void {
    if (this.smoothCurvePoints.length < 2) {
      this.smoothCurvePoints = [];
      this.isSmoothCurveDrawing = false;
      this.renderCanvas();
      return;
    }

    const id = `smooth_curve_${Date.now()}`;
    const curve: SmoothCurveObject = {
      id,
      type: 'SMOOTH_CURVE',
      x: this.smoothCurvePoints[0].x,
      y: this.smoothCurvePoints[0].y,
      points: [...this.smoothCurvePoints],
      tension: 0.5,
      closed: false,
      color: this.color,
      lineWidth: this.lineWidth
    };

    this.saveHistory();
    this.objects.push(curve);
    this.smoothCurvePoints = [];
    this.isSmoothCurveDrawing = false;
    this.selectedId = id;
    this.renderCanvas();
    this.dispatchChangeEvent();
  }

  // 处理贝塞尔曲线点击
  private handleBezierClick(x: number, y: number): void {
    const hitRadius = 8 / this.scale;

    // 检查是否点击了现有点
    for (let i = 0; i < this.bezierPoints.length; i++) {
      const pt = this.bezierPoints[i];

      // 检查是否点击了锚点
      if (Math.hypot(x - pt.x, y - pt.y) < hitRadius) {
        // 如果点击了第一个点且已有多个点，闭合路径
        if (i === 0 && this.bezierPoints.length >= 3) {
          this.finishBezierPath(true);
          return;
        }
        // 否则开始拖拽这个点
        this.bezierDraggingPoint = i;
        this.bezierDraggingHandle = null;
        return;
      }

      // 检查是否点击了控制柄1
      if (pt.cp1x !== undefined && pt.cp1y !== undefined) {
        if (Math.hypot(x - pt.cp1x, y - pt.cp1y) < hitRadius) {
          this.bezierDraggingPoint = i;
          this.bezierDraggingHandle = 'cp1';
          return;
        }
      }

      // 检查是否点击了控制柄2
      if (pt.cp2x !== undefined && pt.cp2y !== undefined) {
        if (Math.hypot(x - pt.cp2x, y - pt.cp2y) < hitRadius) {
          this.bezierDraggingPoint = i;
          this.bezierDraggingHandle = 'cp2';
          return;
        }
      }
    }

    // 添加新点
    const newPoint: BezierPoint = {
      x, y,
      type: 'smooth'
    };

    this.bezierPoints.push(newPoint);
    this.isBezierDrawing = true;
    this.bezierDraggingPoint = this.bezierPoints.length - 1;
    this.bezierDraggingHandle = 'cp2';  // 拖拽出控制柄
    // 立即隐藏空画布提示
    this.hideEmptyHint();

    this.renderCanvas();
  }

  // 完成贝塞尔曲线路径
  private finishBezierPath(closed: boolean): void {
    if (this.bezierPoints.length < 2) {
      this.bezierPoints = [];
      this.isBezierDrawing = false;
      this.renderCanvas();
      return;
    }

    this.saveHistory();

    const bezierObj: BezierObject = {
      id: this.generateId(),
      type: 'BEZIER',
      x: 0,
      y: 0,
      points: [...this.bezierPoints],
      closed,
      color: this.color,
      lineWidth: this.lineWidth
    };

    this.objects.push(bezierObj);
    this.bezierPoints = [];
    this.isBezierDrawing = false;
    this.bezierDraggingPoint = -1;
    this.bezierDraggingHandle = null;

    this.renderCanvas();
    this.dispatchChangeEvent();
  }

  // 绘制贝塞尔曲线编辑状态
  private drawBezierEditState(ctx: CanvasRenderingContext2D): void {
    if (this.bezierPoints.length === 0) return;

    const handleSize = 6 / this.scale;
    const lineWidth = 1 / this.scale;

    // 绘制曲线路径
    ctx.beginPath();
    ctx.strokeStyle = this.color;
    ctx.lineWidth = this.lineWidth;

    const firstPoint = this.bezierPoints[0];
    ctx.moveTo(firstPoint.x, firstPoint.y);

    for (let i = 1; i < this.bezierPoints.length; i++) {
      const prevPoint = this.bezierPoints[i - 1];
      const currPoint = this.bezierPoints[i];

      const cp1x = prevPoint.cp2x ?? prevPoint.x;
      const cp1y = prevPoint.cp2y ?? prevPoint.y;
      const cp2x = currPoint.cp1x ?? currPoint.x;
      const cp2y = currPoint.cp1y ?? currPoint.y;

      ctx.bezierCurveTo(cp1x, cp1y, cp2x, cp2y, currPoint.x, currPoint.y);
    }

    // 绘制到临时预览点
    if (this.bezierTempPoint && this.bezierPoints.length > 0) {
      const lastPoint = this.bezierPoints[this.bezierPoints.length - 1];
      const cp1x = lastPoint.cp2x ?? lastPoint.x;
      const cp1y = lastPoint.cp2y ?? lastPoint.y;
      ctx.bezierCurveTo(cp1x, cp1y, this.bezierTempPoint.x, this.bezierTempPoint.y, this.bezierTempPoint.x, this.bezierTempPoint.y);
    }

    ctx.stroke();

    // 绘制控制柄连线和控制点
    ctx.strokeStyle = 'rgba(59, 130, 246, 0.8)';
    ctx.lineWidth = lineWidth;

    this.bezierPoints.forEach((pt, index) => {
      // 绘制控制柄连线
      if (pt.cp1x !== undefined && pt.cp1y !== undefined) {
        ctx.beginPath();
        ctx.moveTo(pt.x, pt.y);
        ctx.lineTo(pt.cp1x, pt.cp1y);
        ctx.stroke();

        // 绘制控制点
        ctx.fillStyle = '#ffffff';
        ctx.beginPath();
        ctx.arc(pt.cp1x, pt.cp1y, handleSize * 0.7, 0, Math.PI * 2);
        ctx.fill();
        ctx.stroke();
      }

      if (pt.cp2x !== undefined && pt.cp2y !== undefined) {
        ctx.beginPath();
        ctx.moveTo(pt.x, pt.y);
        ctx.lineTo(pt.cp2x, pt.cp2y);
        ctx.stroke();

        // 绘制控制点
        ctx.fillStyle = '#ffffff';
        ctx.beginPath();
        ctx.arc(pt.cp2x, pt.cp2y, handleSize * 0.7, 0, Math.PI * 2);
        ctx.fill();
        ctx.stroke();
      }

      // 绘制锚点
      ctx.fillStyle = index === 0 ? '#ef4444' : '#3b82f6';  // 第一个点红色
      ctx.beginPath();
      ctx.arc(pt.x, pt.y, handleSize, 0, Math.PI * 2);
      ctx.fill();
      ctx.strokeStyle = '#ffffff';
      ctx.lineWidth = 2 / this.scale;
      ctx.stroke();
      ctx.strokeStyle = 'rgba(59, 130, 246, 0.8)';
      ctx.lineWidth = lineWidth;
    });
  }

  // 绘制平滑曲线编辑状态
  private drawSmoothCurveEditState(ctx: CanvasRenderingContext2D): void {
    if (this.smoothCurvePoints.length === 0) return;

    const handleSize = 6 / this.scale;
    const lineWidth = 1 / this.scale;

    // 绘制曲线路径
    ctx.beginPath();
    ctx.strokeStyle = this.color;
    ctx.lineWidth = this.lineWidth;

    if (this.smoothCurvePoints.length === 1) {
      // 只有一个点，绘制一个小圆点
      ctx.arc(this.smoothCurvePoints[0].x, this.smoothCurvePoints[0].y, handleSize, 0, Math.PI * 2);
      ctx.stroke();
    } else {
      // 绘制平滑曲线
      this.drawCatmullRomSpline(ctx, this.smoothCurvePoints, 0.5, false);
      ctx.stroke();
    }

    // 绘制控制点
    this.smoothCurvePoints.forEach((pt, index) => {
      ctx.fillStyle = index === 0 ? 'rgba(34, 197, 94, 0.8)' : 'rgba(59, 130, 246, 0.8)';
      ctx.beginPath();
      ctx.arc(pt.x, pt.y, handleSize, 0, Math.PI * 2);
      ctx.fill();
      ctx.strokeStyle = '#ffffff';
      ctx.lineWidth = 2 / this.scale;
      ctx.stroke();
      ctx.strokeStyle = 'rgba(59, 130, 246, 0.8)';
      ctx.lineWidth = lineWidth;
    });

    // 绘制提示文本
    if (this.smoothCurvePoints.length >= 2) {
      ctx.save();
      ctx.fillStyle = 'rgba(0, 0, 0, 0.7)';
      ctx.font = `${14 / this.scale}px sans-serif`;
      const lastPt = this.smoothCurvePoints[this.smoothCurvePoints.length - 1];
      ctx.fillText(this.t('finishCurve'), lastPt.x + 10 / this.scale, lastPt.y - 10 / this.scale);
      ctx.restore();
    }
  }

  // 画布鼠标移动
  private handleCanvasPointerMove(e: MouseEvent | TouchEvent): void {
    // 处理画布拖拽
    if (this.isPanning) {
      const screenPos = this.getScreenPos(e);
      const dx = screenPos.x - this.panStart.x;
      const dy = screenPos.y - this.panStart.y;
      this.panOffset = { x: this.panOffset.x + dx, y: this.panOffset.y + dy };
      this.panStart = screenPos;
      this.renderCanvas();
      return;
    }

    const { x, y } = this.getMousePos(e);

    // 处理贝塞尔曲线拖拽
    if (this.tool === 'BEZIER' && this.bezierDraggingPoint >= 0) {
      const pt = this.bezierPoints[this.bezierDraggingPoint];
      if (pt) {
        if (this.bezierDraggingHandle === 'cp1') {
          pt.cp1x = x;
          pt.cp1y = y;
          // 对称模式：同步更新 cp2
          if (pt.type === 'symmetric' && pt.cp2x !== undefined && pt.cp2y !== undefined) {
            pt.cp2x = 2 * pt.x - x;
            pt.cp2y = 2 * pt.y - y;
          }
        } else if (this.bezierDraggingHandle === 'cp2') {
          pt.cp2x = x;
          pt.cp2y = y;
          // 对称模式：同步更新 cp1
          if (pt.type === 'symmetric' && pt.cp1x !== undefined && pt.cp1y !== undefined) {
            pt.cp1x = 2 * pt.x - x;
            pt.cp1y = 2 * pt.y - y;
          }
          // 如果是新点，同时设置 cp1 为对称位置
          if (pt.cp1x === undefined) {
            pt.cp1x = 2 * pt.x - x;
            pt.cp1y = 2 * pt.y - y;
            pt.type = 'symmetric';
          }
        } else {
          // 拖拽锚点本身
          const dx = x - pt.x;
          const dy = y - pt.y;
          pt.x = x;
          pt.y = y;
          // 同时移动控制柄
          if (pt.cp1x !== undefined) pt.cp1x += dx;
          if (pt.cp1y !== undefined) pt.cp1y += dy;
          if (pt.cp2x !== undefined) pt.cp2x += dx;
          if (pt.cp2y !== undefined) pt.cp2y += dy;
        }
        this.renderCanvas();
      }
      return;
    }

    // 贝塞尔曲线预览
    if (this.tool === 'BEZIER' && this.bezierPoints.length > 0) {
      this.bezierTempPoint = { x, y, type: 'smooth' };
      this.renderCanvas();
      return;
    }

    if (!this.isDragging || !this.dragStart) return;

    // 处理旋转
    if (this.isRotating && this.selectedId) {
      const obj = this.objects.find(o => o.id === this.selectedId);
      if (obj) {
        const bounds = this.getObjectBounds(obj);
        const centerX = bounds.x + bounds.width / 2;
        const centerY = bounds.y + bounds.height / 2;
        const currentAngle = Math.atan2(y - centerY, x - centerX);
        const deltaAngle = currentAngle - this.rotateStartAngle;
        obj.rotation = this.rotateObjectStartRotation + deltaAngle;
        this.renderCanvas();
      }
      return;
    }

    // 处理斜切
    if (this.isSkewing && this.selectedId && this.skewHandle) {
      const obj = this.objects.find(o => o.id === this.selectedId);
      if (obj) {
        const bounds = this.getObjectBounds(obj);
        const dx = x - this.skewStartPos.x;
        const dy = y - this.skewStartPos.y;

        // 斜切灵敏度
        const sensitivity = 0.002;

        if (this.skewHandle === 'top' || this.skewHandle === 'bottom') {
          // 水平斜切（上下边缘拖拽）
          const direction = this.skewHandle === 'top' ? -1 : 1;
          obj.skewX = this.skewObjectStartSkewX + dx * sensitivity * direction;
          // 限制斜切范围
          obj.skewX = Math.max(-1, Math.min(1, obj.skewX));
        } else {
          // 垂直斜切（左右边缘拖拽）
          const direction = this.skewHandle === 'left' ? -1 : 1;
          obj.skewY = this.skewObjectStartSkewY + dy * sensitivity * direction;
          // 限制斜切范围
          obj.skewY = Math.max(-1, Math.min(1, obj.skewY));
        }
        this.renderCanvas();
      }
      return;
    }

    // 处理变换原点拖拽
    if (this.isDraggingOrigin) {
      this.transformOrigin = { x, y };
      this.renderCanvas();
      return;
    }

    // 处理多选旋转
    if (this.isMultiRotating && this.selectedIds.size >= 2) {
      const origin = this.getMultiTransformOrigin();
      if (origin) {
        const currentAngle = Math.atan2(y - origin.y, x - origin.x);
        const deltaAngle = currentAngle - this.multiRotateStartAngle;

        this.selectedIds.forEach(id => {
          const obj = this.objects.find(o => o.id === id);
          const startState = this.multiRotateObjectsStart.get(id);
          if (obj && startState) {
            // 更新对象自身的旋转
            obj.rotation = startState.rotation + deltaAngle;

            // 围绕变换原点旋转对象位置
            const dx = startState.x - origin.x;
            const dy = startState.y - origin.y;
            const cos = Math.cos(deltaAngle);
            const sin = Math.sin(deltaAngle);
            obj.x = origin.x + dx * cos - dy * sin;
            obj.y = origin.y + dx * sin + dy * cos;
          }
        });

        this.renderCanvas();
      }
      return;
    }

    // 处理调整大小
    if (this.isResizing && this.selectedId && this.resizeHandle && this.resizeStartBounds && this.resizeOriginalObject) {
      const obj = this.objects.find(o => o.id === this.selectedId);
      if (!obj) return;

      // 检测 Shift 键是否按下（用于等比缩放）
      const shiftKey = e instanceof MouseEvent ? e.shiftKey : false;

      let dx = x - this.dragStart.x;
      let dy = y - this.dragStart.y;

      // 如果对象有旋转，将鼠标移动方向转换到对象本地坐标系
      if (obj.rotation) {
        const cos = Math.cos(-obj.rotation);
        const sin = Math.sin(-obj.rotation);
        const rotatedDx = dx * cos - dy * sin;
        const rotatedDy = dx * sin + dy * cos;
        dx = rotatedDx;
        dy = rotatedDy;
      }

      let newX = this.resizeStartBounds.x;
      let newY = this.resizeStartBounds.y;
      let newWidth = this.resizeStartBounds.width;
      let newHeight = this.resizeStartBounds.height;

      if (this.resizeHandle.includes('e')) newWidth = this.resizeStartBounds.width + dx;
      if (this.resizeHandle.includes('w')) {
        newX = this.resizeStartBounds.x + dx;
        newWidth = this.resizeStartBounds.width - dx;
      }
      if (this.resizeHandle.includes('s')) newHeight = this.resizeStartBounds.height + dy;
      if (this.resizeHandle.includes('n')) {
        newY = this.resizeStartBounds.y + dy;
        newHeight = this.resizeStartBounds.height - dy;
      }

      // 等比缩放锁定：按住 Shift 键时保持宽高比
      if (shiftKey && this.resizeStartBounds.width > 0 && this.resizeStartBounds.height > 0) {
        const aspectRatio = this.resizeStartBounds.width / this.resizeStartBounds.height;
        const currentRatio = newWidth / newHeight;

        if (currentRatio > aspectRatio) {
          // 宽度变化更大，以宽度为准调整高度
          const adjustedHeight = newWidth / aspectRatio;
          if (this.resizeHandle.includes('n')) {
            newY = this.resizeStartBounds.y + this.resizeStartBounds.height - adjustedHeight;
          }
          newHeight = adjustedHeight;
        } else {
          // 高度变化更大，以高度为准调整宽度
          const adjustedWidth = newHeight * aspectRatio;
          if (this.resizeHandle.includes('w')) {
            newX = this.resizeStartBounds.x + this.resizeStartBounds.width - adjustedWidth;
          }
          newWidth = adjustedWidth;
        }
      }

      newWidth = Math.max(10, newWidth);
      newHeight = Math.max(10, newHeight);

      // 根据对象类型应用变化
      switch (obj.type) {
        case 'RECTANGLE':
        case 'IMAGE':
          (obj as RectObject | ImageObject).x = newX;
          (obj as RectObject | ImageObject).y = newY;
          (obj as RectObject | ImageObject).width = newWidth;
          (obj as RectObject | ImageObject).height = newHeight;
          break;
        case 'CIRCLE': {
          const radius = Math.max(newWidth, newHeight) / 2;
          (obj as CircleObject).x = newX + radius;
          (obj as CircleObject).y = newY + radius;
          (obj as CircleObject).radius = radius;
          break;
        }
        case 'TEXT': {
          const origT = this.resizeOriginalObject as TextObject;
          const scaleFactor = newWidth / this.resizeStartBounds.width;
          (obj as TextObject).x = newX;
          (obj as TextObject).y = newY + newHeight;
          (obj as TextObject).fontSize = Math.max(8, Math.round(origT.fontSize * scaleFactor));
          break;
        }
        case 'RICH_TEXT': {
          const origRT = this.resizeOriginalObject as RichTextObject;
          const scaleFactor = newWidth / this.resizeStartBounds.width;
          const rtObj = obj as RichTextObject;
          rtObj.x = newX;
          rtObj.y = newY + newHeight;
          rtObj.fontSize = Math.max(8, Math.round(origRT.fontSize * scaleFactor));
          // 按比例调整每个段落的字体大小
          rtObj.segments = origRT.segments.map((seg, i) => ({
            ...seg,
            fontSize: seg.fontSize ? Math.max(8, Math.round(seg.fontSize * scaleFactor)) : undefined
          }));
          break;
        }
        case 'PATH': {
          const origP = this.resizeOriginalObject as PathObject;
          const scaleX = newWidth / this.resizeStartBounds.width;
          const scaleY = newHeight / this.resizeStartBounds.height;
          (obj as PathObject).points = origP.points.map(pt => ({
            x: newX + (pt.x - this.resizeStartBounds!.x) * scaleX,
            y: newY + (pt.y - this.resizeStartBounds!.y) * scaleY
          }));
          break;
        }
        case 'GROUP': {
          const origG = this.resizeOriginalObject as GroupObject;
          const scaleX = newWidth / this.resizeStartBounds.width;
          const scaleY = newHeight / this.resizeStartBounds.height;
          const g = obj as GroupObject;

          // 更新 GROUP 的中心点和尺寸
          g.x = newX + newWidth / 2;
          g.y = newY + newHeight / 2;
          g.width = newWidth;
          g.height = newHeight;

          // 按比例缩放所有子对象
          g.children = origG.children.map(child => {
            const scaledChild = JSON.parse(JSON.stringify(child));

            // 缩放位置（相对于中心点）
            scaledChild.x = child.x * scaleX;
            scaledChild.y = child.y * scaleY;

            // 根据子对象类型缩放尺寸
            if (scaledChild.type === 'RECTANGLE' || scaledChild.type === 'IMAGE') {
              scaledChild.width = (child as RectObject).width * scaleX;
              scaledChild.height = (child as RectObject).height * scaleY;
            } else if (scaledChild.type === 'CIRCLE') {
              scaledChild.radius = (child as CircleObject).radius * Math.min(scaleX, scaleY);
            } else if (scaledChild.type === 'TEXT') {
              scaledChild.fontSize = Math.max(8, Math.round((child as TextObject).fontSize * Math.min(scaleX, scaleY)));
            } else if (scaledChild.type === 'RICH_TEXT') {
              const rtChild = child as RichTextObject;
              scaledChild.fontSize = Math.max(8, Math.round(rtChild.fontSize * Math.min(scaleX, scaleY)));
              scaledChild.segments = rtChild.segments.map((seg: RichTextSegment) => ({
                ...seg,
                fontSize: seg.fontSize ? Math.max(8, Math.round(seg.fontSize * Math.min(scaleX, scaleY))) : undefined
              }));
            } else if (scaledChild.type === 'LINE' || scaledChild.type === 'ARROW') {
              scaledChild.x2 = (child as LineObject).x2 * scaleX;
              scaledChild.y2 = (child as LineObject).y2 * scaleY;
            } else if (scaledChild.type === 'PATH') {
              scaledChild.points = (child as PathObject).points.map((pt: Point) => ({
                x: pt.x * scaleX,
                y: pt.y * scaleY
              }));
            }

            return scaledChild;
          });
          break;
        }
      }

      this.renderCanvas();
      return;
    }

    // 框选模式
    if (this.isSelecting && this.selectionRect) {
      this.selectionRect.width = x - this.selectionRect.x;
      this.selectionRect.height = y - this.selectionRect.y;
      this.renderCanvas();
      return;
    }

    // 多选拖动模式
    if (this.isMultiDragging && this.selectedIds.size > 0) {
      const dx = x - this.multiDragStart.x;
      const dy = y - this.multiDragStart.y;

      this.selectedIds.forEach(id => {
        const obj = this.objects.find(o => o.id === id);
        if (obj) {
          if (obj.type === 'PATH') {
            const p = obj as PathObject;
            p.points = p.points.map(pt => ({ x: pt.x + dx, y: pt.y + dy }));
          } else if (obj.type === 'LINE' || obj.type === 'ARROW') {
            const lineObj = obj as LineObject | ArrowObject;
            lineObj.x += dx;
            lineObj.y += dy;
            lineObj.x2 += dx;
            lineObj.y2 += dy;
          } else {
            obj.x += dx;
            obj.y += dy;
          }
        }
      });

      this.multiDragStart = { x, y };
      this.renderCanvas();
      return;
    }

    // 移动选中对象（单选）
    if (this.tool === 'SELECT' && this.selectedId) {
      const obj = this.objects.find(o => o.id === this.selectedId);
      if (obj) {
        if (obj.type === 'PATH') {
          const p = obj as PathObject;
          const dx = x - this.dragStart.x;
          const dy = y - this.dragStart.y;
          p.points = p.points.map(pt => ({ x: pt.x + dx, y: pt.y + dy }));
          this.dragStart = { x, y };
        } else {
          obj.x = x - this.dragOffset.x;
          obj.y = y - this.dragOffset.y;
        }
        this.renderCanvas();
      }
    } else if (this.currentObject) {
      // 更新正在绘制的图形
      if (this.currentObject.type === 'RECTANGLE') {
        (this.currentObject as RectObject).width = x - this.currentObject.x;
        (this.currentObject as RectObject).height = y - this.currentObject.y;
      } else if (this.currentObject.type === 'CIRCLE') {
        const radius = Math.sqrt(Math.pow(x - this.currentObject.x, 2) + Math.pow(y - this.currentObject.y, 2));
        (this.currentObject as CircleObject).radius = radius;
      } else if (this.currentObject.type === 'PATH') {
        (this.currentObject as PathObject).points.push({ x, y });
      } else if (this.currentObject.type === 'LINE') {
        (this.currentObject as LineObject).x2 = x;
        (this.currentObject as LineObject).y2 = y;
      } else if (this.currentObject.type === 'ARROW') {
        (this.currentObject as ArrowObject).x2 = x;
        (this.currentObject as ArrowObject).y2 = y;
      } else if (this.currentObject.type === 'POLYGON') {
        const radius = Math.sqrt(Math.pow(x - this.currentObject.x, 2) + Math.pow(y - this.currentObject.y, 2));
        (this.currentObject as PolygonObject).radius = radius;
      } else if (this.currentObject.type === 'TRIANGLE') {
        const radius = Math.sqrt(Math.pow(x - this.currentObject.x, 2) + Math.pow(y - this.currentObject.y, 2));
        (this.currentObject as TriangleObject).radius = radius;
      } else if (this.currentObject.type === 'STAR') {
        const outerRadius = Math.sqrt(Math.pow(x - this.currentObject.x, 2) + Math.pow(y - this.currentObject.y, 2));
        (this.currentObject as StarObject).outerRadius = outerRadius;
        (this.currentObject as StarObject).innerRadius = outerRadius * 0.4; // 内半径为外半径的40%
      } else if (this.currentObject.type === 'HEART') {
        const size = Math.sqrt(Math.pow(x - this.currentObject.x, 2) + Math.pow(y - this.currentObject.y, 2));
        (this.currentObject as HeartObject).size = size;
      } else if (this.currentObject.type === 'DIAMOND') {
        const width = Math.abs(x - this.currentObject.x) * 2;
        const height = Math.abs(y - this.currentObject.y) * 2;
        (this.currentObject as DiamondObject).width = width;
        (this.currentObject as DiamondObject).height = height;
      }
      this.renderCanvas();
    }
  }

  // 画布鼠标抬起
  private handleCanvasPointerUp(): void {
    // 结束贝塞尔曲线拖拽
    if (this.tool === 'BEZIER' && this.bezierDraggingPoint >= 0) {
      this.bezierDraggingPoint = -1;
      this.bezierDraggingHandle = null;
      return;
    }

    // 结束框选
    if (this.isSelecting && this.selectionRect) {
      const rect = this.normalizeRect(this.selectionRect);
      // 选中框内的所有对象
      this.clearSelection();
      this.objects.forEach(obj => {
        if (obj.visible === false) return;
        const bounds = this.getObjectBounds(obj);
        if (this.rectsIntersect(rect, bounds)) {
          this.selectedIds.add(obj.id);
        }
      });
      this.selectedId = null;
      this.selectionRect = null;
      this.isSelecting = false;
    }

    // 检测是否有数据变化（拖动、调整大小、旋转、斜切等操作）
    const hasDataChanged = this.isResizing || this.isRotating || this.isSkewing ||
                           this.isMultiDragging || this.isMultiRotating ||
                           (this.isDragging && this.selectedId && this.dragStart);

    this.isDragging = false;
    this.dragStart = null;
    this.isResizing = false;
    this.resizeHandle = null;
    this.resizeStartBounds = null;
    this.resizeOriginalObject = null;
    this.isRotating = false;
    this.isSkewing = false;
    this.skewHandle = null;
    this.isPanning = false;
    this.isMultiDragging = false;
    this.isDraggingOrigin = false;
    this.isMultiRotating = false;
    this.multiRotateObjectsStart.clear();

    // 恢复光标
    if (this.isSpacePressed) {
      this.canvas.style.cursor = 'grab';
    } else {
      this.canvas.style.cursor = this.tool === 'SELECT' ? 'default' : 'crosshair';
    }

    if (this.currentObject) {
      this.objects.push(this.currentObject);
      this.currentObject = null;
      this.dispatchChangeEvent();
    } else if (hasDataChanged) {
      // 拖动、调整大小、旋转等操作完成后触发变化事件
      this.dispatchChangeEvent();
    }

    this.renderCanvas();
    this.updateUI();
  }

  // 标准化矩形（处理负宽高）
  private normalizeRect(rect: { x: number; y: number; width: number; height: number }): { x: number; y: number; width: number; height: number } {
    return {
      x: rect.width < 0 ? rect.x + rect.width : rect.x,
      y: rect.height < 0 ? rect.y + rect.height : rect.y,
      width: Math.abs(rect.width),
      height: Math.abs(rect.height)
    };
  }

  // 判断两个矩形是否相交
  private rectsIntersect(a: { x: number; y: number; width: number; height: number }, b: { x: number; y: number; width: number; height: number }): boolean {
    return !(a.x + a.width < b.x || b.x + b.width < a.x || a.y + a.height < b.y || b.y + b.height < a.y);
  }

  // 双击编辑文本
  private handleCanvasDoubleClick(e: MouseEvent): void {
    e.preventDefault();
    const { x, y } = this.getMousePos(e);

    const clickedObject = [...this.objects].reverse().find(obj => this.isHit(obj, x, y));

    if (clickedObject) {
      if (clickedObject.type === 'TEXT') {
        const textObj = clickedObject as TextObject;
        this.editingTextId = textObj.id;
        this.textInputPos = { x: textObj.x, y: textObj.y };
        const screenX = textObj.x * this.scale + this.panOffset.x;
        const screenY = textObj.y * this.scale + this.panOffset.y;
        this.showTextInput(screenX, screenY, textObj.text);
        this.setTool('SELECT');
      } else if (clickedObject.type === 'RICH_TEXT') {
        // 双击编辑富文本
        const rtObj = clickedObject as RichTextObject;
        this.showRichTextEditor(rtObj.x, rtObj.y, rtObj);
        this.setTool('SELECT');
      }
    }
  }

  // ========== 公开 API 方法 ==========

  /**
   * 设置图片大小限制
   * @param size 大小限制，支持数字（KB）或字符串（如 "500kb", "2mb"）
   * @example
   * editor.setMaxImageSize(500);      // 500 KB
   * editor.setMaxImageSize('500kb');  // 500 KB
   * editor.setMaxImageSize('2mb');    // 2 MB
   * editor.setMaxImageSize(0);        // 取消限制
   */
  public setMaxImageSize(size: number | string): void {
    if (typeof size === 'number') {
      this.config.maxImageSize = size;
    } else {
      const value = size.trim().toLowerCase();
      const num = parseFloat(value);

      if (isNaN(num) || num <= 0) {
        this.config.maxImageSize = 0;
        return;
      }

      if (value.endsWith('mb')) {
        this.config.maxImageSize = num * 1024;
      } else {
        this.config.maxImageSize = num;
      }
    }
  }

  /**
   * 获取当前图片大小限制（单位：KB）
   * @returns 当前限制值，0 表示无限制
   */
  public getMaxImageSize(): number {
    return this.config.maxImageSize || 0;
  }

  // 导出画布数据为 JSON 对象
  // Public API: re-sync the internal canvas with the host size (e.g. after the
  // host element is resized externally, which does not fire window `resize`).
  public resize(): void {
    this.initCanvas(false);
  }

  // Public API: remove all objects without the confirm popup.
  public clear(): void {
    this.clearCanvas();
  }

  public exportJSON(): { version: string; objects: any[] } {
    return {
      version: '1.0',
      objects: this.objects.map(obj => {
        const { imageElement, ...rest } = obj as ImageObject;
        return rest;
      })
    };
  }

  // 导出画布为 PNG 图片（下载文件）
  public exportPNG(filename: string = 'canvas-export.png'): void {
    if (!this.canvas) return;

    const link = document.createElement('a');
    link.download = filename;
    link.href = this.canvas.toDataURL('image/png');
    link.click();
  }

  // 获取画布图片数据（返回 base64 或 Blob）
  public getImageData(options: {
    format?: 'png' | 'jpeg' | 'webp';
    quality?: number;
    type?: 'dataURL' | 'blob';
    background?: string;
  } = {}): Promise<string | Blob> {
    return new Promise((resolve, reject) => {
      if (!this.canvas) {
        reject(new Error('Canvas not initialized'));
        return;
      }

      const {
        format = 'png',
        quality = 0.92,
        type = 'dataURL',
        background = '#ffffff'
      } = options;

      // 创建临时画布
      const tempCanvas = document.createElement('canvas');
      tempCanvas.width = this.canvas.width;
      tempCanvas.height = this.canvas.height;
      const tempCtx = tempCanvas.getContext('2d')!;

      // 绘制背景
      tempCtx.fillStyle = background;
      tempCtx.fillRect(0, 0, tempCanvas.width, tempCanvas.height);

      // 应用缩放和平移
      tempCtx.translate(this.panOffset.x, this.panOffset.y);
      tempCtx.scale(this.scale, this.scale);

      // 绘制所有对象
      this.objects.forEach(obj => this.drawObject(tempCtx, obj));

      const mimeType = `image/${format}`;

      if (type === 'blob') {
        tempCanvas.toBlob(
          (blob) => {
            if (blob) {
              resolve(blob);
            } else {
              reject(new Error('Failed to create blob'));
            }
          },
          mimeType,
          quality
        );
      } else {
        resolve(tempCanvas.toDataURL(mimeType, quality));
      }
    });
  }

  // ========== Tween动画系统方法 ==========

  // 创建动画 (使用 tweenAnimate 避免与 HTMLElement.animate 冲突)
  public tweenAnimate(objectId: string, toProps: TweenProps, config: TweenConfig = {}): string {
    const obj = this.objects.find(o => o.id === objectId);
    if (!obj) {
      console.error(`Object with id ${objectId} not found`);
      return '';
    }

    // 获取当前属性作为起始值
    const fromProps: TweenProps = {};
    for (const key of Object.keys(toProps)) {
      if (key in obj) {
        fromProps[key] = (obj as any)[key];
      }
    }

    // 解析缓动函数
    let easingFn: EasingFunction = Easing.linear;
    if (config.easing) {
      if (typeof config.easing === 'function') {
        easingFn = config.easing;
      } else if (config.easing in Easing) {
        easingFn = Easing[config.easing];
      }
    }

    const tweenId = `tween_${Date.now()}_${Math.random().toString(36).substr(2, 9)}`;
    const tween: TweenInstance = {
      id: tweenId,
      objectId,
      fromProps,
      toProps,
      config: {
        duration: config.duration || 1000,
        delay: config.delay || 0,
        easing: easingFn,
        repeat: config.repeat || 0,
        yoyo: config.yoyo || false,
        onStart: config.onStart,
        onUpdate: config.onUpdate,
        onComplete: config.onComplete
      },
      startTime: performance.now() + (config.delay || 0),
      currentRepeat: 0,
      isReversed: false,
      isStarted: false,
      isCompleted: false
    };

    this.tweens.set(tweenId, tween);
    this.startAnimationLoop();
    return tweenId;
  }

  // 停止指定动画
  public stopAnimation(tweenId: string): void {
    this.tweens.delete(tweenId);
    if (this.tweens.size === 0) {
      this.stopAnimationLoop();
    }
  }

  // 停止对象的所有动画
  public stopObjectAnimations(objectId: string): void {
    for (const [id, tween] of this.tweens) {
      if (tween.objectId === objectId) {
        this.tweens.delete(id);
      }
    }
    if (this.tweens.size === 0) {
      this.stopAnimationLoop();
    }
  }

  // 停止所有动画
  public stopAllAnimations(): void {
    this.tweens.clear();
    this.stopAnimationLoop();
  }

  // 启动动画循环
  private startAnimationLoop(): void {
    if (this.isAnimating) return;
    this.isAnimating = true;
    this.animationLoop();
  }

  // 停止动画循环
  private stopAnimationLoop(): void {
    this.isAnimating = false;
    if (this.animationFrameId !== null) {
      cancelAnimationFrame(this.animationFrameId);
      this.animationFrameId = null;
    }
  }

  // 动画循环
  private animationLoop = (): void => {
    if (!this.isAnimating) return;

    const now = performance.now();
    let hasActiveTweens = false;
    const completedTweens: string[] = [];

    for (const [id, tween] of this.tweens) {
      if (tween.isCompleted) continue;

      // 检查是否还在延迟阶段
      if (now < tween.startTime) {
        hasActiveTweens = true;
        continue;
      }

      // 触发开始回调
      if (!tween.isStarted) {
        tween.isStarted = true;
        tween.config.onStart?.();
      }

      const elapsed = now - tween.startTime;
      let progress = Math.min(1, elapsed / tween.config.duration);

      // 应用缓动
      let easedProgress = (tween.config.easing as EasingFunction)(progress);

      // yoyo模式处理
      if (tween.isReversed) {
        easedProgress = 1 - easedProgress;
      }

      // 更新对象属性
      const obj = this.objects.find(o => o.id === tween.objectId);
      if (obj) {
        for (const key of Object.keys(tween.toProps)) {
          const from = tween.fromProps[key] ?? 0;
          const to = tween.toProps[key] ?? 0;
          (obj as any)[key] = from + (to - from) * easedProgress;
        }
      }

      // 触发更新回调
      tween.config.onUpdate?.(progress);

      // 检查动画是否完成
      if (progress >= 1) {
        if (tween.config.repeat === -1 || tween.currentRepeat < tween.config.repeat) {
          // 需要重复
          tween.currentRepeat++;
          tween.startTime = now;
          if (tween.config.yoyo) {
            tween.isReversed = !tween.isReversed;
          }
          hasActiveTweens = true;
        } else {
          // 动画完成
          tween.isCompleted = true;
          tween.config.onComplete?.();
          completedTweens.push(id);
        }
      } else {
        hasActiveTweens = true;
      }
    }

    // 清理完成的动画
    for (const id of completedTweens) {
      this.tweens.delete(id);
    }

    // 渲染画布
    this.renderCanvas();

    // 继续循环
    if (hasActiveTweens || this.tweens.size > 0) {
      this.animationFrameId = requestAnimationFrame(this.animationLoop);
    } else {
      this.stopAnimationLoop();
    }
  }

  // ========== 触摸手势处理方法 ==========

  // 处理触摸开始
  private handleTouchStart(e: TouchEvent): void {
    e.preventDefault();
    const rect = this.canvas.getBoundingClientRect();
    const now = performance.now();

    // 清除惯性动画
    if (this.inertiaAnimationId !== null) {
      cancelAnimationFrame(this.inertiaAnimationId);
      this.inertiaAnimationId = null;
    }

    // 记录所有触摸点
    for (let i = 0; i < e.changedTouches.length; i++) {
      const touch = e.changedTouches[i];
      const x = touch.clientX - rect.left;
      const y = touch.clientY - rect.top;
      this.touchState.touches.set(touch.identifier, {
        id: touch.identifier,
        x, y,
        startX: x,
        startY: y,
        timestamp: now
      });
    }

    const touchCount = this.touchState.touches.size;

    if (touchCount === 1) {
      // 单指触摸 - 启动长按检测
      this.touchState.gestureType = 'none';
      this.touchState.longPressTimer = window.setTimeout(() => {
        if (this.touchState.touches.size === 1 && this.touchState.gestureType === 'none') {
          this.touchState.gestureType = 'longPress';
          this.handleLongPress();
        }
      }, this.LONG_PRESS_DURATION);

      // 转发为鼠标事件进行普通处理
      const touch = e.touches[0];
      const fakeEvent = this.createFakeMouseEvent(touch, rect);
      this.handleCanvasPointerDown(fakeEvent);

    } else if (touchCount === 2) {
      // 双指触摸 - 初始化捏合/旋转手势
      this.clearLongPressTimer();
      const touches = Array.from(this.touchState.touches.values());
      this.touchState.initialDistance = this.getDistance(touches[0], touches[1]);
      this.touchState.initialAngle = this.getAngle(touches[0], touches[1]);
      this.touchState.initialScale = this.scale;
      this.touchState.initialRotation = this.selectedId ?
        (this.objects.find(o => o.id === this.selectedId)?.rotation || 0) : 0;
      this.touchState.gestureType = 'pinch';
    }
  }

  // 处理触摸移动
  private handleTouchMove(e: TouchEvent): void {
    e.preventDefault();
    const rect = this.canvas.getBoundingClientRect();
    const now = performance.now();

    // 更新触摸点位置
    for (let i = 0; i < e.changedTouches.length; i++) {
      const touch = e.changedTouches[i];
      const existing = this.touchState.touches.get(touch.identifier);
      if (existing) {
        const prevX = existing.x;
        const prevY = existing.y;
        existing.x = touch.clientX - rect.left;
        existing.y = touch.clientY - rect.top;
        // 计算速度（用于惯性）
        const dt = (now - existing.timestamp) / 1000;
        if (dt > 0) {
          this.touchState.velocity = {
            x: (existing.x - prevX) / dt,
            y: (existing.y - prevY) / dt
          };
        }
        existing.timestamp = now;
      }
    }

    const touchCount = this.touchState.touches.size;

    if (touchCount === 1) {
      // 单指拖动
      const touch = e.touches[0];
      const touchPoint = this.touchState.touches.get(touch.identifier);
      if (touchPoint) {
        const dx = Math.abs(touchPoint.x - touchPoint.startX);
        const dy = Math.abs(touchPoint.y - touchPoint.startY);
        // 移动超过阈值，取消长按检测
        if (dx > 10 || dy > 10) {
          this.clearLongPressTimer();
          this.touchState.gestureType = 'drag';
        }
      }
      // 转发为鼠标事件
      const fakeEvent = this.createFakeMouseEvent(touch, rect);
      this.handleCanvasPointerMove(fakeEvent);

    } else if (touchCount === 2 && this.touchState.gestureType === 'pinch') {
      // 双指手势处理
      const touches = Array.from(this.touchState.touches.values());
      const currentDistance = this.getDistance(touches[0], touches[1]);
      const currentAngle = this.getAngle(touches[0], touches[1]);

      // 计算缩放比例
      const scaleDelta = currentDistance / this.touchState.initialDistance;
      // 计算旋转角度
      const angleDelta = currentAngle - this.touchState.initialAngle;

      // 计算双指中心点
      const centerX = (touches[0].x + touches[1].x) / 2;
      const centerY = (touches[0].y + touches[1].y) / 2;

      if (this.selectedId && this.tool === 'SELECT') {
        // 缩放/旋转选中的对象
        const obj = this.objects.find(o => o.id === this.selectedId);
        if (obj) {
          // 应用旋转
          obj.rotation = this.touchState.initialRotation + angleDelta * (180 / Math.PI);
          this.renderCanvas();
        }
      } else {
        // 缩放画布
        const newScale = Math.max(0.1, Math.min(5, this.touchState.initialScale * scaleDelta));
        this.scale = newScale;
        this.renderCanvas();
        this.updateZoomDisplay();
      }
    }
  }

  // 处理触摸结束
  private handleTouchEnd(e: TouchEvent): void {
    e.preventDefault();
    this.clearLongPressTimer();

    // 移除结束的触摸点
    for (let i = 0; i < e.changedTouches.length; i++) {
      const touch = e.changedTouches[i];
      this.touchState.touches.delete(touch.identifier);
    }

    // 如果所有触摸都结束
    if (this.touchState.touches.size === 0) {
      // 应用惯性滑动（如果是拖动手势）
      if (this.touchState.gestureType === 'drag' && !this.selectedId) {
        this.applyInertia();
      }

      // 重置状态
      this.touchState.gestureType = 'none';
      this.handleCanvasPointerUp();
    }
  }

  // 处理长按
  private handleLongPress(): void {
    const touches = Array.from(this.touchState.touches.values());
    if (touches.length === 1) {
      const touch = touches[0];
      const { x, y } = this.screenToCanvas(touch.x, touch.y);

      // 查找被点击的对象
      for (let i = this.objects.length - 1; i >= 0; i--) {
        const obj = this.objects[i];
        if (this.isHit(obj, x, y)) {
          // 选中对象
          this.selectedId = obj.id;
          this.selectedIds.clear();
          this.selectedIds.add(obj.id);
          this.renderCanvas();

          // 触发选中事件
          this.dispatchEvent(new CustomEvent('object-selected', {
            detail: { object: obj, longPress: true }
          }));
          break;
        }
      }
    }
  }

  // 应用惯性滑动
  private applyInertia(): void {
    const friction = 0.95;
    const minVelocity = 0.5;
    let { x: vx, y: vy } = this.touchState.velocity;

    const animate = () => {
      if (Math.abs(vx) < minVelocity && Math.abs(vy) < minVelocity) {
        this.inertiaAnimationId = null;
        return;
      }

      this.panOffset.x += vx * 0.016; // 假设 60fps
      this.panOffset.y += vy * 0.016;
      vx *= friction;
      vy *= friction;

      this.renderCanvas();
      this.inertiaAnimationId = requestAnimationFrame(animate);
    };

    this.inertiaAnimationId = requestAnimationFrame(animate);
  }

  // 清除长按定时器
  private clearLongPressTimer(): void {
    if (this.touchState.longPressTimer !== null) {
      clearTimeout(this.touchState.longPressTimer);
      this.touchState.longPressTimer = null;
    }
  }

  // 计算两点之间的距离
  private getDistance(p1: TouchPoint, p2: TouchPoint): number {
    return Math.sqrt(Math.pow(p2.x - p1.x, 2) + Math.pow(p2.y - p1.y, 2));
  }

  // 计算两点之间的角度
  private getAngle(p1: TouchPoint, p2: TouchPoint): number {
    return Math.atan2(p2.y - p1.y, p2.x - p1.x);
  }

  // 创建模拟鼠标事件
  private createFakeMouseEvent(touch: Touch, rect: DOMRect): MouseEvent {
    return {
      clientX: touch.clientX,
      clientY: touch.clientY,
      offsetX: touch.clientX - rect.left,
      offsetY: touch.clientY - rect.top,
      button: 0,
      preventDefault: () => {},
      stopPropagation: () => {}
    } as unknown as MouseEvent;
  }

  // ========== 富文本编辑器方法 ==========

  // 绑定富文本编辑器事件
  private bindRichTextEditorEvents(): void {
    // 工具栏按钮事件
    this.richTextEditor.querySelectorAll('.rich-text-btn').forEach(btn => {
      btn.addEventListener('click', (e) => {
        const action = (e.currentTarget as HTMLElement).dataset.action;
        this.handleRichTextToolbarAction(action || '');
      });
    });

    // 颜色选择
    const colorInput = this.richTextEditor.querySelector('.rich-text-color') as HTMLInputElement;
    colorInput?.addEventListener('input', () => {
      if (this.selectedSegmentIndex >= 0 && this.richTextSegments[this.selectedSegmentIndex]) {
        this.richTextSegments[this.selectedSegmentIndex].color = colorInput.value;
        this.updateRichTextSegmentsUI();
      }
    });

    // 字号选择
    const fontSizeInput = this.richTextEditor.querySelector('.rich-text-fontsize') as HTMLInputElement;
    fontSizeInput?.addEventListener('change', () => {
      if (this.selectedSegmentIndex >= 0 && this.richTextSegments[this.selectedSegmentIndex]) {
        this.richTextSegments[this.selectedSegmentIndex].fontSize = parseInt(fontSizeInput.value) || 16;
        this.updateRichTextSegmentsUI();
      }
    });

    // 取消按钮
    this.richTextEditor.querySelector('.rich-text-action-btn.cancel')?.addEventListener('click', () => {
      this.hideRichTextEditor();
    });

    // 确定按钮
    this.richTextEditor.querySelector('.rich-text-action-btn.confirm')?.addEventListener('click', () => {
      this.confirmRichText();
    });
  }

  // 处理富文本工具栏操作
  private handleRichTextToolbarAction(action: string): void {
    if (this.selectedSegmentIndex < 0 || !this.richTextSegments[this.selectedSegmentIndex]) {
      if (action === 'add-segment') {
        this.addRichTextSegment();
      }
      return;
    }

    const segment = this.richTextSegments[this.selectedSegmentIndex];
    switch (action) {
      case 'bold':
        segment.bold = !segment.bold;
        break;
      case 'italic':
        segment.italic = !segment.italic;
        break;
      case 'add-segment':
        this.addRichTextSegment();
        break;
    }
    this.updateRichTextSegmentsUI();
    this.updateRichTextToolbarState();
  }

  // 添加富文本段落
  private addRichTextSegment(): void {
    const newSegment: RichTextSegment = {
      text: '',
      color: '#000000',
      fontSize: 16,
      bold: false,
      italic: false
    };
    this.richTextSegments.push(newSegment);
    this.selectedSegmentIndex = this.richTextSegments.length - 1;
    this.updateRichTextSegmentsUI();
    this.updateRichTextToolbarState();
  }

  // 删除富文本段落
  private deleteRichTextSegment(index: number): void {
    if (this.richTextSegments.length <= 1) return;
    this.richTextSegments.splice(index, 1);
    if (this.selectedSegmentIndex >= this.richTextSegments.length) {
      this.selectedSegmentIndex = this.richTextSegments.length - 1;
    }
    this.updateRichTextSegmentsUI();
  }

  // 更新富文本段落UI
  private updateRichTextSegmentsUI(): void {
    const container = this.richTextEditor.querySelector('.rich-text-segments');
    if (!container) return;

    container.innerHTML = this.richTextSegments.map((seg, idx) => `
      <div class="rich-text-segment ${idx === this.selectedSegmentIndex ? 'selected' : ''}" data-index="${idx}">
        <input type="text" class="rich-text-segment-input" value="${this.escapeHtml(seg.text)}" data-index="${idx}" placeholder="${this.t('richTextPlaceholder') || '请输入文字'}">
        <span class="rich-text-segment-preview" style="
          color: ${seg.color || '#000000'};
          font-weight: ${seg.bold ? 'bold' : 'normal'};
          font-style: ${seg.italic ? 'italic' : 'normal'};
          font-size: ${Math.min(seg.fontSize || 16, 14)}px;
        ">${seg.bold ? 'B' : ''}${seg.italic ? 'I' : ''} ${seg.fontSize || 16}px</span>
        <button class="rich-text-segment-delete" data-index="${idx}" ${this.richTextSegments.length <= 1 ? 'disabled' : ''}>
          <svg viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" stroke-width="2">
            <line x1="18" y1="6" x2="6" y2="18"/><line x1="6" y1="6" x2="18" y2="18"/>
          </svg>
        </button>
      </div>
    `).join('');

    // 绑定输入事件 - 先绑定输入框事件，阻止冒泡
    container.querySelectorAll('.rich-text-segment-input').forEach(input => {
      // 阻止点击事件冒泡，避免触发父级点击导致UI重建
      input.addEventListener('click', (e) => {
        e.stopPropagation();
      });
      // 阻止mousedown冒泡，防止失焦
      input.addEventListener('mousedown', (e) => {
        e.stopPropagation();
      });
      // 聚焦时选中当前段落，但不重建UI
      input.addEventListener('focus', (e) => {
        const idx = parseInt((e.target as HTMLElement).dataset.index || '0');
        if (this.selectedSegmentIndex !== idx) {
          this.selectedSegmentIndex = idx;
          // 只更新选中状态的样式，不重建整个UI
          container.querySelectorAll('.rich-text-segment').forEach((seg, i) => {
            seg.classList.toggle('selected', i === idx);
          });
          this.updateRichTextToolbarState();
        }
      });
      // 输入时更新数据
      input.addEventListener('input', (e) => {
        const idx = parseInt((e.target as HTMLElement).dataset.index || '0');
        this.richTextSegments[idx].text = (e.target as HTMLInputElement).value;
      });
    });

    // 绑定段落事件 - 点击段落非输入框区域时选中
    container.querySelectorAll('.rich-text-segment').forEach(el => {
      el.addEventListener('click', (e) => {
        const target = e.target as HTMLElement;
        // 如果点击的是输入框或删除按钮，不处理
        if (target.closest('.rich-text-segment-input') || target.closest('.rich-text-segment-delete')) return;
        const index = parseInt((el as HTMLElement).dataset.index || '0');
        if (this.selectedSegmentIndex !== index) {
          this.selectedSegmentIndex = index;
          // 只更新选中状态的样式，不重建整个UI
          container.querySelectorAll('.rich-text-segment').forEach((seg, i) => {
            seg.classList.toggle('selected', i === index);
          });
          this.updateRichTextToolbarState();
        }
      });
    });

    // 绑定删除事件
    container.querySelectorAll('.rich-text-segment-delete').forEach(btn => {
      btn.addEventListener('click', (e) => {
        e.stopPropagation();
        const idx = parseInt((e.currentTarget as HTMLElement).dataset.index || '0');
        this.deleteRichTextSegment(idx);
      });
    });
  }

  // 更新工具栏状态
  private updateRichTextToolbarState(): void {
    if (this.selectedSegmentIndex < 0 || !this.richTextSegments[this.selectedSegmentIndex]) return;

    const segment = this.richTextSegments[this.selectedSegmentIndex];

    // 更新按钮状态
    this.richTextEditor.querySelector('.rich-text-btn[data-action="bold"]')?.classList.toggle('active', !!segment.bold);
    this.richTextEditor.querySelector('.rich-text-btn[data-action="italic"]')?.classList.toggle('active', !!segment.italic);

    // 更新颜色
    const colorInput = this.richTextEditor.querySelector('.rich-text-color') as HTMLInputElement;
    if (colorInput) colorInput.value = segment.color || '#000000';

    // 更新字号
    const fontSizeInput = this.richTextEditor.querySelector('.rich-text-fontsize') as HTMLInputElement;
    if (fontSizeInput) fontSizeInput.value = String(segment.fontSize || 16);
  }

  // 显示富文本编辑器
  public showRichTextEditor(x: number, y: number, existingObject?: RichTextObject): void {
    this.richTextPosition = { x, y };

    if (existingObject) {
      this.editingRichTextId = existingObject.id;
      this.richTextSegments = JSON.parse(JSON.stringify(existingObject.segments));
    } else {
      this.editingRichTextId = null;
      this.richTextSegments = [{ text: '', color: '#000000', fontSize: 16, bold: false, italic: false }];
    }

    this.selectedSegmentIndex = 0;

    // 定位编辑器
    const canvasRect = this.canvas.getBoundingClientRect();
    const screenX = x * this.scale + this.panOffset.x;
    const screenY = y * this.scale + this.panOffset.y;

    this.richTextEditor.style.display = 'block';
    this.richTextEditor.style.left = `${Math.min(screenX, canvasRect.width - 340)}px`;
    this.richTextEditor.style.top = `${Math.min(screenY, canvasRect.height - 250)}px`;

    this.updateRichTextSegmentsUI();
    this.updateRichTextToolbarState();
  }

  // 隐藏富文本编辑器
  public hideRichTextEditor(): void {
    this.richTextEditor.style.display = 'none';
    this.richTextSegments = [];
    this.selectedSegmentIndex = -1;
    this.editingRichTextId = null;
  }

  // 确认富文本
  private confirmRichText(): void {
    // 过滤空段落
    const validSegments = this.richTextSegments.filter(s => s.text.trim());
    if (validSegments.length === 0) {
      this.hideRichTextEditor();
      this.setTool('SELECT');
      return;
    }

    if (this.editingRichTextId) {
      // 编辑现有对象
      const obj = this.objects.find(o => o.id === this.editingRichTextId) as RichTextObject;
      if (obj) {
        obj.segments = validSegments;
        this.saveHistory();
        this.renderCanvas();
        this.dispatchChangeEvent();
      }
    } else {
      // 创建新对象
      const richTextObj: RichTextObject = {
        id: this.generateId(),
        type: 'RICH_TEXT',
        x: this.richTextPosition.x,
        y: this.richTextPosition.y,
        segments: validSegments,
        fontSize: 16,
        color: this.color,
        lineWidth: this.lineWidth,
        rotation: 0
      };
      this.objects.push(richTextObj);
      this.saveHistory();
      this.renderCanvas();
      this.dispatchChangeEvent();
      const emptyHint2 = this.shadow.querySelector('.empty-hint') as HTMLElement;
      if (emptyHint2) emptyHint2.style.display = 'none';
    }

    this.hideRichTextEditor();
    // 确认后切换到选择模式
    this.setTool('SELECT');
  }

  // HTML转义
  private escapeHtml(text: string): string {
    const div = document.createElement('div');
    div.textContent = text;
    return div.innerHTML;
  }

  // ========== 时间线编辑器方法 ==========

  // 绑定时间线编辑器事件
  private bindTimelineEditorEvents(): void {
    // 播放按钮
    this.timelineEditor.querySelector('.timeline-btn.play')?.addEventListener('click', () => {
      this.playTimeline();
    });

    // 暂停按钮
    this.timelineEditor.querySelector('.timeline-btn.pause')?.addEventListener('click', () => {
      this.pauseTimeline();
    });

    // 停止按钮
    this.timelineEditor.querySelector('.timeline-btn.stop')?.addEventListener('click', () => {
      this.stopTimeline();
    });

    // 关闭按钮
    this.timelineEditor.querySelector('.timeline-close')?.addEventListener('click', () => {
      this.hideTimelineEditor();
    });

    // 添加关键帧按钮
    this.timelineEditor.querySelector('.timeline-add-keyframe')?.addEventListener('click', () => {
      this.addKeyframeAtCurrentTime();
    });

    // 时间轴点击定位
    const ruler = this.timelineEditor.querySelector('.timeline-ruler');
    ruler?.addEventListener('click', (e) => {
      const rect = (ruler as HTMLElement).getBoundingClientRect();
      const x = (e as MouseEvent).clientX - rect.left;
      const percent = x / rect.width;
      this.seekTimeline(percent * this.timelineDuration);
    });
  }

  // 显示时间线编辑器
  public showTimelineEditor(): void {
    this.timelineEditor.style.display = 'block';
    this.updateTimelineTracks();
  }

  // 隐藏时间线编辑器
  public hideTimelineEditor(): void {
    this.timelineEditor.style.display = 'none';
    this.stopTimeline();
  }

  // 更新时间线轨道显示
  private updateTimelineTracks(): void {
    const tracksContainer = this.timelineEditor.querySelector('.timeline-tracks');
    if (!tracksContainer) return;

    // 获取有动画的对象
    const tweenArray = Array.from(this.tweens.values());
    const animatedObjectIds = new Set(tweenArray.map(t => t.objectId));
    const animatedObjects = this.objects.filter(obj => animatedObjectIds.has(obj.id));

    if (animatedObjects.length === 0) {
      tracksContainer.innerHTML = `<div style="padding: 20px; text-align: center; color: #64748b; font-size: 13px;">
        ${this.t('noAnimations') || '暂无动画，选择对象后使用 tweenAnimate() 方法添加动画'}
      </div>`;
      return;
    }

    let html = '';
    animatedObjects.forEach(obj => {
      const objTweens = tweenArray.filter(t => t.objectId === obj.id);
      html += `<div class="timeline-track">
        <span class="timeline-track-label">${obj.type}</span>
        <div class="timeline-track-bar">`;

      objTweens.forEach(tween => {
        const delay = tween.config.delay || 0;
        const duration = tween.config.duration || 1000;
        const startPercent = (delay / this.timelineDuration) * 100;
        const widthPercent = (duration / this.timelineDuration) * 100;
        html += `<div class="timeline-keyframe" style="left: ${startPercent}%" title="${duration}ms"></div>`;
        html += `<div class="timeline-keyframe" style="left: ${startPercent + widthPercent}%" title="End"></div>`;
      });

      html += `</div></div>`;
    });

    tracksContainer.innerHTML = html;
  }

  // 播放时间线
  private playTimeline(): void {
    if (this.timelineIsPlaying) return;
    this.timelineIsPlaying = true;

    const startTime = performance.now() - this.timelineCurrentTime;

    const animate = () => {
      if (!this.timelineIsPlaying) return;

      this.timelineCurrentTime = performance.now() - startTime;

      if (this.timelineCurrentTime >= this.timelineDuration) {
        this.timelineCurrentTime = 0;
        this.stopTimeline();
        return;
      }

      this.updateTimelinePlayhead();
      requestAnimationFrame(animate);
    };

    requestAnimationFrame(animate);
  }

  // 暂停时间线
  private pauseTimeline(): void {
    this.timelineIsPlaying = false;
  }

  // 停止时间线
  private stopTimeline(): void {
    this.timelineIsPlaying = false;
    this.timelineCurrentTime = 0;
    this.updateTimelinePlayhead();
  }

  // 定位时间线
  private seekTimeline(time: number): void {
    this.timelineCurrentTime = Math.max(0, Math.min(time, this.timelineDuration));
    this.updateTimelinePlayhead();
  }

  // 更新播放头位置
  private updateTimelinePlayhead(): void {
    const playhead = this.timelineEditor.querySelector('.timeline-playhead') as HTMLElement;
    const timeDisplay = this.timelineEditor.querySelector('.timeline-time') as HTMLElement;

    if (playhead) {
      const percent = (this.timelineCurrentTime / this.timelineDuration) * 100;
      playhead.style.left = `${percent}%`;
    }

    if (timeDisplay) {
      timeDisplay.textContent = `${(this.timelineCurrentTime / 1000).toFixed(2)}s`;
    }
  }

  // 在当前时间添加关键帧
  private addKeyframeAtCurrentTime(): void {
    if (!this.selectedId) {
      console.error('Please select an object first');
      return;
    }

    const obj = this.objects.find(o => o.id === this.selectedId);
    if (!obj) return;

    // 创建一个简单的位置动画作为示例
    this.tweenAnimate(this.selectedId, {
      x: obj.x + 100,
      y: obj.y
    }, {
      duration: 1000,
      delay: this.timelineCurrentTime,
      easing: 'easeInOutQuad'
    });

    this.updateTimelineTracks();
  }

  // 渲染画布
  private renderCanvas(): void {
    if (!this.ctx) return;

    // 清空画布
    this.ctx.clearRect(0, 0, this.canvas.width, this.canvas.height);

    // 绘制背景 (skipped in transparent/overlay mode, e.g. when overlaying media for annotation)
    if (!this.hasAttribute('transparent') && !this.hasAttribute('overlay')) {
      this.ctx.fillStyle = '#ffffff';
      this.ctx.fillRect(0, 0, this.canvas.width, this.canvas.height);
    }

    // 应用缩放和平移
    this.ctx.save();
    this.ctx.translate(this.panOffset.x, this.panOffset.y);
    this.ctx.scale(this.scale, this.scale);

    // 绘制所有对象
    this.objects.forEach(obj => this.drawObject(this.ctx, obj));

    // 绘制正在创建的对象
    if (this.currentObject) {
      this.drawObject(this.ctx, this.currentObject);
    }

    // 绘制贝塞尔曲线编辑状态
    if (this.tool === 'BEZIER' && this.bezierPoints.length > 0) {
      this.drawBezierEditState(this.ctx);
    }

    // 绘制平滑曲线编辑状态
    if (this.tool === 'SMOOTH_CURVE' && this.smoothCurvePoints.length > 0) {
      this.drawSmoothCurveEditState(this.ctx);
    }

    // 绘制选中对象的调整手柄
    if (this.selectedId && this.tool === 'SELECT') {
      const selectedObj = this.objects.find(o => o.id === this.selectedId);
      if (selectedObj) {
        this.drawSelectionHandles(this.ctx, selectedObj);
      }
    }

    // 绘制多选对象的高亮框
    if (this.selectedIds.size > 0 && this.tool === 'SELECT') {
      this.selectedIds.forEach(id => {
        const obj = this.objects.find(o => o.id === id);
        if (obj) {
          const bounds = this.getObjectBounds(obj);
          this.ctx.save();
          // 应用与对象相同的变换
          if (obj.rotation || obj.skewX || obj.skewY) {
            const centerX = bounds.x + bounds.width / 2;
            const centerY = bounds.y + bounds.height / 2;
            this.ctx.translate(centerX, centerY);
            if (obj.rotation) {
              this.ctx.rotate(obj.rotation);
            }
            if (obj.skewX || obj.skewY) {
              this.ctx.transform(1, obj.skewY || 0, obj.skewX || 0, 1, 0, 0);
            }
            this.ctx.translate(-centerX, -centerY);
          }
          this.ctx.strokeStyle = 'rgba(139, 92, 246, 0.8)';
          this.ctx.lineWidth = 2 / this.scale;
          this.ctx.setLineDash([5 / this.scale, 5 / this.scale]);
          this.ctx.strokeRect(bounds.x - 2, bounds.y - 2, bounds.width + 4, bounds.height + 4);
          this.ctx.setLineDash([]);
          this.ctx.restore();
        }
      });

      // 绘制多选整体边界框和变换原点
      if (this.selectedIds.size >= 2) {
        const multiBounds = this.getMultiSelectionBounds();
        if (multiBounds) {
          // 绘制整体边界框
          this.ctx.strokeStyle = 'rgba(59, 130, 246, 0.8)';
          this.ctx.lineWidth = 2 / this.scale;
          this.ctx.setLineDash([8 / this.scale, 4 / this.scale]);
          this.ctx.strokeRect(multiBounds.x - 5, multiBounds.y - 5, multiBounds.width + 10, multiBounds.height + 10);
          this.ctx.setLineDash([]);

          // 绘制变换原点
          const origin = this.getMultiTransformOrigin();
          if (origin) {
            const originSize = 8 / this.scale;

            // 外圈
            this.ctx.beginPath();
            this.ctx.strokeStyle = '#3b82f6';
            this.ctx.lineWidth = 2 / this.scale;
            this.ctx.arc(origin.x, origin.y, originSize, 0, Math.PI * 2);
            this.ctx.stroke();

            // 十字线
            this.ctx.beginPath();
            this.ctx.moveTo(origin.x - originSize * 1.5, origin.y);
            this.ctx.lineTo(origin.x + originSize * 1.5, origin.y);
            this.ctx.moveTo(origin.x, origin.y - originSize * 1.5);
            this.ctx.lineTo(origin.x, origin.y + originSize * 1.5);
            this.ctx.stroke();

            // 中心点
            this.ctx.beginPath();
            this.ctx.fillStyle = this.transformOrigin ? '#ef4444' : '#3b82f6'; // 自定义原点显示红色
            this.ctx.arc(origin.x, origin.y, 3 / this.scale, 0, Math.PI * 2);
            this.ctx.fill();
          }
        }
      }
    }

    // 绘制框选矩形
    if (this.isSelecting && this.selectionRect) {
      this.ctx.strokeStyle = 'rgba(139, 92, 246, 0.8)';
      this.ctx.fillStyle = 'rgba(139, 92, 246, 0.1)';
      this.ctx.lineWidth = 1 / this.scale;
      this.ctx.setLineDash([5 / this.scale, 5 / this.scale]);
      this.ctx.strokeRect(this.selectionRect.x, this.selectionRect.y, this.selectionRect.width, this.selectionRect.height);
      this.ctx.fillRect(this.selectionRect.x, this.selectionRect.y, this.selectionRect.width, this.selectionRect.height);
      this.ctx.setLineDash([]);
    }

    this.ctx.restore();
  }

  // 绘制单个对象
  private drawObject(ctx: CanvasRenderingContext2D, obj: CanvasObject): void {
    // 检查可见性
    if (obj.visible === false) return;

    ctx.save();

    // 应用透明度
    if (obj.opacity !== undefined && obj.opacity !== 1) {
      ctx.globalAlpha = obj.opacity;
    }

    // 应用旋转和斜切变换
    if (obj.rotation || obj.skewX || obj.skewY) {
      const bounds = this.getObjectBounds(obj);
      const centerX = bounds.x + bounds.width / 2;
      const centerY = bounds.y + bounds.height / 2;
      ctx.translate(centerX, centerY);
      if (obj.rotation) {
        ctx.rotate(obj.rotation);
      }
      if (obj.skewX || obj.skewY) {
        // 应用斜切变换矩阵
        ctx.transform(1, obj.skewY || 0, obj.skewX || 0, 1, 0, 0);
      }
      ctx.translate(-centerX, -centerY);
    }

    ctx.beginPath();
    ctx.strokeStyle = obj.color;
    ctx.lineWidth = obj.lineWidth;
    ctx.fillStyle = obj.color;

    // 选中高亮（单选或多选）
    if (obj.id === this.selectedId || this.selectedIds.has(obj.id)) {
      ctx.shadowColor = 'rgba(139, 92, 246, 0.5)';
      ctx.shadowBlur = 10;
    } else {
      ctx.shadowBlur = 0;
    }

    switch (obj.type) {
      case 'RECTANGLE': {
        const r = obj as RectObject;
        // 应用线条样式
        this.applyLineStyle(ctx, r.lineStyle);
        const fillMode = r.fillMode || 'stroke';
        if (fillMode === 'fill' || fillMode === 'both') {
          ctx.fillRect(r.x, r.y, r.width, r.height);
        }
        if (fillMode === 'stroke' || fillMode === 'both') {
          ctx.strokeRect(r.x, r.y, r.width, r.height);
        }
        ctx.setLineDash([]); // 重置虚线
        break;
      }
      case 'CIRCLE': {
        const c = obj as CircleObject;
        // 应用线条样式
        this.applyLineStyle(ctx, c.lineStyle);
        ctx.beginPath();
        ctx.arc(c.x, c.y, c.radius, 0, 2 * Math.PI);
        const fillMode = c.fillMode || 'stroke';
        if (fillMode === 'fill' || fillMode === 'both') {
          ctx.fill();
        }
        if (fillMode === 'stroke' || fillMode === 'both') {
          ctx.stroke();
        }
        ctx.setLineDash([]); // 重置虚线
        break;
      }
      case 'PATH': {
        const p = obj as PathObject;
        if (p.points.length < 2) break;
        ctx.beginPath();
        ctx.lineCap = 'round';
        ctx.lineJoin = 'round';
        ctx.moveTo(p.points[0].x, p.points[0].y);
        for (let i = 1; i < p.points.length; i++) {
          ctx.lineTo(p.points[i].x, p.points[i].y);
        }
        ctx.stroke();
        break;
      }
      case 'TEXT': {
        const t = obj as TextObject;
        const fontStyle = `${t.bold ? 'bold ' : ''}${t.italic ? 'italic ' : ''}`;
        ctx.font = `${fontStyle}${t.fontSize}px ${t.fontFamily || 'sans-serif'}`;
        ctx.textAlign = 'center';
        ctx.textBaseline = 'middle';
        ctx.fillText(t.text, t.x, t.y);
        // 重置对齐方式
        ctx.textAlign = 'left';
        ctx.textBaseline = 'alphabetic';

        // 如果启用热区功能且文本有热区配置，绘制热区标识
        if (this.config.enableHotzone && t.hotzone) {
          const textWidth = this.measureTextWidth(t.text, t.fontSize);
          const themeColor = this.config.themeColor || DEFAULT_THEME_COLOR;

          // 绘制虚线边框
          ctx.save();
          ctx.strokeStyle = themeColor;
          ctx.lineWidth = 1;
          ctx.setLineDash([4, 2]);
          ctx.strokeRect(t.x - textWidth / 2 - 2, t.y - t.fontSize / 2 - 2, textWidth + 4, t.fontSize + 4);
          ctx.setLineDash([]);

          // 绘制热区图标（小圆点）
          ctx.fillStyle = themeColor;
          ctx.beginPath();
          ctx.arc(t.x + textWidth / 2 + 8, t.y, 4, 0, 2 * Math.PI);
          ctx.fill();
          ctx.restore();
        }
        break;
      }
      case 'RICH_TEXT': {
        const rt = obj as RichTextObject;
        const defaultFontFamily = rt.fontFamily || 'sans-serif';

        // 计算总宽度用于居中对齐
        let totalWidth = 0;
        for (const segment of rt.segments) {
          const fontSize = segment.fontSize || rt.fontSize;
          const fontFamily = segment.fontFamily || defaultFontFamily;
          const bold = segment.bold ? 'bold ' : '';
          const italic = segment.italic ? 'italic ' : '';
          ctx.font = `${italic}${bold}${fontSize}px ${fontFamily}`;
          totalWidth += ctx.measureText(segment.text).width;
        }

        // 根据 textAlign 计算起始 X 坐标
        let startX = rt.x;
        if (rt.textAlign === 'center') {
          startX = rt.x - totalWidth / 2;
        } else if (rt.textAlign === 'right') {
          startX = rt.x - totalWidth;
        }

        // 计算最大字体大小用于垂直居中
        const maxFontSize = this.getRichTextMaxFontSize(rt);
        // 垂直居中：baseY 是文本基线位置
        const baseY = rt.textAlign === 'center' ? rt.y + maxFontSize / 3 : rt.y;

        let offsetX = 0;

        for (const segment of rt.segments) {
          const fontSize = segment.fontSize || rt.fontSize;
          const fontFamily = segment.fontFamily || defaultFontFamily;
          const bold = segment.bold ? 'bold ' : '';
          const italic = segment.italic ? 'italic ' : '';

          // 设置字体样式
          ctx.font = `${italic}${bold}${fontSize}px ${fontFamily}`;

          // 绘制背景色（高亮）
          if (segment.backgroundColor) {
            const segmentWidth = ctx.measureText(segment.text).width;
            ctx.fillStyle = segment.backgroundColor;
            ctx.fillRect(startX + offsetX, baseY - fontSize, segmentWidth, fontSize * 1.2);
          }

          // 设置文本颜色
          ctx.fillStyle = segment.color || rt.color;

          // 绘制文本
          ctx.fillText(segment.text, startX + offsetX, baseY);

          // 获取文本宽度用于后续装饰线和偏移计算
          const textWidth = ctx.measureText(segment.text).width;

          // 绘制下划线
          if (segment.underline) {
            ctx.beginPath();
            ctx.strokeStyle = segment.color || rt.color;
            ctx.lineWidth = Math.max(1, fontSize / 12);
            ctx.moveTo(startX + offsetX, baseY + fontSize * 0.1);
            ctx.lineTo(startX + offsetX + textWidth, baseY + fontSize * 0.1);
            ctx.stroke();
          }

          // 绘制删除线
          if (segment.strikethrough) {
            ctx.beginPath();
            ctx.strokeStyle = segment.color || rt.color;
            ctx.lineWidth = Math.max(1, fontSize / 12);
            ctx.moveTo(startX + offsetX, baseY - fontSize * 0.35);
            ctx.lineTo(startX + offsetX + textWidth, baseY - fontSize * 0.35);
            ctx.stroke();
          }

          offsetX += textWidth;
        }
        break;
      }
      case 'IMAGE': {
        const imgObj = obj as ImageObject;
        if (imgObj.imageElement && imgObj.imageElement.complete) {
          // 应用滤镜
          if (imgObj.filters) {
            const filterParts: string[] = [];
            if (imgObj.filters.brightness !== undefined && imgObj.filters.brightness !== 100) {
              filterParts.push(`brightness(${imgObj.filters.brightness}%)`);
            }
            if (imgObj.filters.contrast !== undefined && imgObj.filters.contrast !== 100) {
              filterParts.push(`contrast(${imgObj.filters.contrast}%)`);
            }
            if (imgObj.filters.blur !== undefined && imgObj.filters.blur > 0) {
              filterParts.push(`blur(${imgObj.filters.blur}px)`);
            }
            if (imgObj.filters.grayscale !== undefined && imgObj.filters.grayscale > 0) {
              filterParts.push(`grayscale(${imgObj.filters.grayscale}%)`);
            }
            if (imgObj.filters.saturate !== undefined && imgObj.filters.saturate !== 100) {
              filterParts.push(`saturate(${imgObj.filters.saturate}%)`);
            }
            if (filterParts.length > 0) {
              ctx.filter = filterParts.join(' ');
            }
          }
          ctx.drawImage(imgObj.imageElement, imgObj.x, imgObj.y, imgObj.width, imgObj.height);
          ctx.filter = 'none'; // 重置滤镜
        } else if (imgObj.dataUrl) {
          // 加载图片
          const img = new Image();
          img.onload = () => {
            imgObj.imageElement = img;
            this.renderCanvas();
          };
          img.src = imgObj.dataUrl;
        }
        break;
      }
      case 'LINE': {
        const l = obj as LineObject;
        // 应用线条样式
        this.applyLineStyle(ctx, l.lineStyle);
        ctx.beginPath();
        ctx.moveTo(l.x, l.y);
        ctx.lineTo(l.x2, l.y2);
        ctx.stroke();
        ctx.setLineDash([]); // 重置虚线
        break;
      }
      case 'ARROW': {
        const a = obj as ArrowObject;
        // 应用线条样式
        this.applyLineStyle(ctx, a.lineStyle);
        ctx.beginPath();
        ctx.moveTo(a.x, a.y);
        ctx.lineTo(a.x2, a.y2);
        ctx.stroke();
        ctx.setLineDash([]); // 重置虚线绘制箭头头部

        // 绘制箭头头部
        const angle = Math.atan2(a.y2 - a.y, a.x2 - a.x);
        const headLength = 15;
        const arrowType = a.arrowType || 'single';

        // 绘制终点箭头（单向或双向都有）
        if (arrowType !== 'none') {
          ctx.beginPath();
          ctx.moveTo(a.x2, a.y2);
          ctx.lineTo(a.x2 - headLength * Math.cos(angle - Math.PI / 6), a.y2 - headLength * Math.sin(angle - Math.PI / 6));
          ctx.moveTo(a.x2, a.y2);
          ctx.lineTo(a.x2 - headLength * Math.cos(angle + Math.PI / 6), a.y2 - headLength * Math.sin(angle + Math.PI / 6));
          ctx.stroke();
        }

        // 绘制起点箭头（双向箭头）
        if (arrowType === 'double') {
          const reverseAngle = angle + Math.PI;
          ctx.beginPath();
          ctx.moveTo(a.x, a.y);
          ctx.lineTo(a.x - headLength * Math.cos(reverseAngle - Math.PI / 6), a.y - headLength * Math.sin(reverseAngle - Math.PI / 6));
          ctx.moveTo(a.x, a.y);
          ctx.lineTo(a.x - headLength * Math.cos(reverseAngle + Math.PI / 6), a.y - headLength * Math.sin(reverseAngle + Math.PI / 6));
          ctx.stroke();
        }
        break;
      }
      case 'POLYGON': {
        const pg = obj as PolygonObject;
        if (pg.radius <= 0) break;
        // 应用线条样式
        this.applyLineStyle(ctx, pg.lineStyle);
        ctx.beginPath();
        for (let i = 0; i < pg.sides; i++) {
          const angle = (2 * Math.PI / pg.sides) * i - Math.PI / 2;
          const px = pg.x + pg.radius * Math.cos(angle);
          const py = pg.y + pg.radius * Math.sin(angle);
          if (i === 0) {
            ctx.moveTo(px, py);
          } else {
            ctx.lineTo(px, py);
          }
        }
        ctx.closePath();
        const fillMode = pg.fillMode || 'stroke';
        if (fillMode === 'fill' || fillMode === 'both') {
          ctx.fill();
        }
        if (fillMode === 'stroke' || fillMode === 'both') {
          ctx.stroke();
        }
        ctx.setLineDash([]); // 重置虚线
        break;
      }
      case 'TRIANGLE': {
        const tri = obj as TriangleObject;
        if (tri.radius <= 0) break;
        // 应用线条样式
        this.applyLineStyle(ctx, tri.lineStyle);
        ctx.beginPath();
        for (let i = 0; i < 3; i++) {
          const angle = (2 * Math.PI / 3) * i - Math.PI / 2;
          const px = tri.x + tri.radius * Math.cos(angle);
          const py = tri.y + tri.radius * Math.sin(angle);
          if (i === 0) {
            ctx.moveTo(px, py);
          } else {
            ctx.lineTo(px, py);
          }
        }
        ctx.closePath();
        const fillMode = tri.fillMode || 'stroke';
        if (fillMode === 'fill' || fillMode === 'both') {
          ctx.fill();
        }
        if (fillMode === 'stroke' || fillMode === 'both') {
          ctx.stroke();
        }
        ctx.setLineDash([]); // 重置虚线
        break;
      }
      case 'STAR': {
        const star = obj as StarObject;
        if (star.outerRadius <= 0) break;
        // 应用线条样式
        this.applyLineStyle(ctx, star.lineStyle);
        ctx.beginPath();
        const starPoints = star.points || 5;
        for (let i = 0; i < starPoints * 2; i++) {
          const angle = (Math.PI / starPoints) * i - Math.PI / 2;
          const radius = i % 2 === 0 ? star.outerRadius : star.innerRadius;
          const px = star.x + radius * Math.cos(angle);
          const py = star.y + radius * Math.sin(angle);
          if (i === 0) {
            ctx.moveTo(px, py);
          } else {
            ctx.lineTo(px, py);
          }
        }
        ctx.closePath();
        const fillMode = star.fillMode || 'stroke';
        if (fillMode === 'fill' || fillMode === 'both') {
          ctx.fill();
        }
        if (fillMode === 'stroke' || fillMode === 'both') {
          ctx.stroke();
        }
        ctx.setLineDash([]); // 重置虚线
        break;
      }
      case 'HEART': {
        const heart = obj as HeartObject;
        if (heart.size <= 0) break;
        // 应用线条样式
        this.applyLineStyle(ctx, heart.lineStyle);
        const size = heart.size;
        ctx.beginPath();
        // 心形路径
        ctx.moveTo(heart.x, heart.y + size * 0.3);
        // 左半边
        ctx.bezierCurveTo(
          heart.x - size * 0.5, heart.y - size * 0.3,
          heart.x - size, heart.y + size * 0.3,
          heart.x, heart.y + size
        );
        // 右半边
        ctx.bezierCurveTo(
          heart.x + size, heart.y + size * 0.3,
          heart.x + size * 0.5, heart.y - size * 0.3,
          heart.x, heart.y + size * 0.3
        );
        ctx.closePath();
        const fillMode = heart.fillMode || 'stroke';
        if (fillMode === 'fill' || fillMode === 'both') {
          ctx.fill();
        }
        if (fillMode === 'stroke' || fillMode === 'both') {
          ctx.stroke();
        }
        ctx.setLineDash([]); // 重置虚线
        break;
      }
      case 'DIAMOND': {
        const diamond = obj as DiamondObject;
        if (diamond.width <= 0 || diamond.height <= 0) break;
        // 应用线条样式
        this.applyLineStyle(ctx, diamond.lineStyle);
        ctx.beginPath();
        ctx.moveTo(diamond.x, diamond.y - diamond.height / 2); // 上
        ctx.lineTo(diamond.x + diamond.width / 2, diamond.y);  // 右
        ctx.lineTo(diamond.x, diamond.y + diamond.height / 2); // 下
        ctx.lineTo(diamond.x - diamond.width / 2, diamond.y);  // 左
        ctx.closePath();
        const fillMode = diamond.fillMode || 'stroke';
        if (fillMode === 'fill' || fillMode === 'both') {
          ctx.fill();
        }
        if (fillMode === 'stroke' || fillMode === 'both') {
          ctx.stroke();
        }
        ctx.setLineDash([]); // 重置虚线
        break;
      }
      case 'BEZIER': {
        const bezier = obj as BezierObject;
        if (bezier.points.length < 2) break;

        ctx.beginPath();
        const firstPoint = bezier.points[0];
        ctx.moveTo(firstPoint.x, firstPoint.y);

        for (let i = 1; i < bezier.points.length; i++) {
          const prevPoint = bezier.points[i - 1];
          const currPoint = bezier.points[i];

          // 使用贝塞尔曲线连接
          const cp1x = prevPoint.cp2x ?? prevPoint.x;
          const cp1y = prevPoint.cp2y ?? prevPoint.y;
          const cp2x = currPoint.cp1x ?? currPoint.x;
          const cp2y = currPoint.cp1y ?? currPoint.y;

          ctx.bezierCurveTo(cp1x, cp1y, cp2x, cp2y, currPoint.x, currPoint.y);
        }

        // 如果闭合，连接回起点
        if (bezier.closed && bezier.points.length > 2) {
          const lastPoint = bezier.points[bezier.points.length - 1];
          const cp1x = lastPoint.cp2x ?? lastPoint.x;
          const cp1y = lastPoint.cp2y ?? lastPoint.y;
          const cp2x = firstPoint.cp1x ?? firstPoint.x;
          const cp2y = firstPoint.cp1y ?? firstPoint.y;
          ctx.bezierCurveTo(cp1x, cp1y, cp2x, cp2y, firstPoint.x, firstPoint.y);
          ctx.closePath();
        }

        // 填充（如果有）
        if (bezier.fill && bezier.closed) {
          ctx.fillStyle = bezier.fill;
          ctx.fill();
        }
        ctx.stroke();
        break;
      }
      case 'SMOOTH_CURVE': {
        const curve = obj as SmoothCurveObject;
        if (curve.points.length < 2) break;

        ctx.beginPath();
        this.drawCatmullRomSpline(ctx, curve.points, curve.tension || 0.5, curve.closed || false);
        ctx.stroke();
        break;
      }
      case 'GROUP': {
        const g = obj as GroupObject;
        // 绘制组合中的所有子对象（将相对坐标转换为绝对坐标）
        g.children.forEach(child => {
          const offsetChild = { ...child, x: child.x + g.x, y: child.y + g.y };
          // 处理 LINE 和 ARROW 的 x2, y2
          if (child.type === 'LINE' || child.type === 'ARROW') {
            (offsetChild as LineObject | ArrowObject).x2 = (child as LineObject | ArrowObject).x2 + g.x;
            (offsetChild as LineObject | ArrowObject).y2 = (child as LineObject | ArrowObject).y2 + g.y;
          }
          // 处理 PATH 的 points
          if (child.type === 'PATH') {
            (offsetChild as PathObject).points = (child as PathObject).points.map(pt => ({ x: pt.x + g.x, y: pt.y + g.y }));
          }
          this.drawObject(ctx, offsetChild as CanvasObject);
        });
        break;
      }
    }

    ctx.restore();
  }

  // 绘制选中手柄
  private drawSelectionHandles(ctx: CanvasRenderingContext2D, obj: CanvasObject): void {
    const bounds = this.getObjectBounds(obj);
    const handleSize = 8;
    const rotateHandleDistance = 30; // 旋转手柄距离对象顶部的距离

    ctx.save();

    // 应用旋转和斜切变换
    const centerX = bounds.x + bounds.width / 2;
    const centerY = bounds.y + bounds.height / 2;
    if (obj.rotation || obj.skewX || obj.skewY) {
      ctx.translate(centerX, centerY);
      if (obj.rotation) {
        ctx.rotate(obj.rotation);
      }
      if (obj.skewX || obj.skewY) {
        ctx.transform(1, obj.skewY || 0, obj.skewX || 0, 1, 0, 0);
      }
      ctx.translate(-centerX, -centerY);
    }

    ctx.shadowBlur = 0;
    ctx.fillStyle = '#3b82f6';
    ctx.strokeStyle = '#ffffff';
    ctx.lineWidth = 2;

    // 绘制角落手柄
    const corners = [
      { x: bounds.x, y: bounds.y },
      { x: bounds.x + bounds.width, y: bounds.y },
      { x: bounds.x, y: bounds.y + bounds.height },
      { x: bounds.x + bounds.width, y: bounds.y + bounds.height },
    ];

    corners.forEach(corner => {
      ctx.beginPath();
      ctx.rect(corner.x - handleSize / 2, corner.y - handleSize / 2, handleSize, handleSize);
      ctx.fill();
      ctx.stroke();
    });

    // 绘制选择边框
    ctx.strokeStyle = '#3b82f6';
    ctx.lineWidth = 1;
    ctx.setLineDash([5, 5]);
    ctx.strokeRect(bounds.x, bounds.y, bounds.width, bounds.height);
    ctx.setLineDash([]);

    // 绘制旋转手柄
    const rotateHandleY = bounds.y - rotateHandleDistance;

    // 连接线
    ctx.beginPath();
    ctx.strokeStyle = '#3b82f6';
    ctx.lineWidth = 1;
    ctx.setLineDash([]);
    ctx.moveTo(centerX, bounds.y);
    ctx.lineTo(centerX, rotateHandleY);
    ctx.stroke();

    // 旋转手柄圆形
    ctx.beginPath();
    ctx.fillStyle = '#3b82f6';
    ctx.strokeStyle = '#ffffff';
    ctx.lineWidth = 2;
    ctx.arc(centerX, rotateHandleY, 6, 0, Math.PI * 2);
    ctx.fill();
    ctx.stroke();

    // 绘制旋转图标（圆弧箭头）
    ctx.beginPath();
    ctx.strokeStyle = '#ffffff';
    ctx.lineWidth = 1.5;
    ctx.arc(centerX, rotateHandleY, 3, -Math.PI * 0.7, Math.PI * 0.3);
    ctx.stroke();

    // 绘制斜切手柄（边缘中点的菱形）- 仅当对象足够大时显示
    const minSizeForSkewHandles = 30; // 最小尺寸阈值
    if (bounds.width >= minSizeForSkewHandles && bounds.height >= minSizeForSkewHandles) {
      const skewHandleSize = 6;
      const skewHandles = [
        { x: centerX, y: bounds.y, type: 'top' },                           // 上
        { x: centerX, y: bounds.y + bounds.height, type: 'bottom' },        // 下
        { x: bounds.x, y: centerY, type: 'left' },                          // 左
        { x: bounds.x + bounds.width, y: centerY, type: 'right' },          // 右
      ];

      ctx.fillStyle = '#f59e0b'; // 橙色区分斜切手柄
      ctx.strokeStyle = '#ffffff';
      ctx.lineWidth = 1.5;

      skewHandles.forEach(handle => {
        ctx.beginPath();
        // 绘制菱形
        ctx.moveTo(handle.x, handle.y - skewHandleSize);
        ctx.lineTo(handle.x + skewHandleSize, handle.y);
        ctx.lineTo(handle.x, handle.y + skewHandleSize);
        ctx.lineTo(handle.x - skewHandleSize, handle.y);
        ctx.closePath();
        ctx.fill();
        ctx.stroke();
      });
    }

    ctx.restore();
  }

  // 获取斜切手柄位置
  private getSkewHandleAtPoint(obj: CanvasObject, x: number, y: number): 'top' | 'bottom' | 'left' | 'right' | null {
    const bounds = this.getObjectBounds(obj);

    // 如果对象太小，不启用斜切手柄
    const minSizeForSkewHandles = 30;
    if (bounds.width < minSizeForSkewHandles || bounds.height < minSizeForSkewHandles) {
      return null;
    }

    const centerX = bounds.x + bounds.width / 2;
    const centerY = bounds.y + bounds.height / 2;
    const handleSize = 10; // 点击区域

    // 如果对象有旋转或斜切，需要将点击坐标反向变换
    let localX = x;
    let localY = y;
    if (obj.rotation || obj.skewX || obj.skewY) {
      let dx = x - centerX;
      let dy = y - centerY;

      // 反向旋转
      if (obj.rotation) {
        const cos = Math.cos(-obj.rotation);
        const sin = Math.sin(-obj.rotation);
        const newDx = dx * cos - dy * sin;
        const newDy = dx * sin + dy * cos;
        dx = newDx;
        dy = newDy;
      }

      // 反向斜切（逆矩阵）
      if (obj.skewX || obj.skewY) {
        const skewX = obj.skewX || 0;
        const skewY = obj.skewY || 0;
        const det = 1 - skewX * skewY;
        if (Math.abs(det) > 0.001) {
          const newDx = (dx - skewX * dy) / det;
          const newDy = (dy - skewY * dx) / det;
          dx = newDx;
          dy = newDy;
        }
      }

      localX = centerX + dx;
      localY = centerY + dy;
    }

    const handles = [
      { x: centerX, y: bounds.y, type: 'top' as const },
      { x: centerX, y: bounds.y + bounds.height, type: 'bottom' as const },
      { x: bounds.x, y: centerY, type: 'left' as const },
      { x: bounds.x + bounds.width, y: centerY, type: 'right' as const },
    ];

    for (const handle of handles) {
      if (Math.abs(localX - handle.x) <= handleSize && Math.abs(localY - handle.y) <= handleSize) {
        return handle.type;
      }
    }
    return null;
  }

  // 图片上传处理
  private handleImageUpload(e: Event): void {
    const input = e.target as HTMLInputElement;
    if (!input.files || input.files.length === 0) return;

    const file = input.files[0];

    // 检查图片大小限制
    const maxSizeKB = this.config.maxImageSize || 0;
    if (maxSizeKB > 0) {
      const fileSizeKB = file.size / 1024;
      if (fileSizeKB > maxSizeKB) {
        this.showImageSizeError(fileSizeKB, maxSizeKB);
        input.value = '';
        return;
      }
    }

    const reader = new FileReader();

    reader.onload = (event) => {
      const dataUrl = event.target?.result as string;
      const img = new Image();
      img.onload = () => {
        this.saveHistory();
        const maxSize = 300;
        let width = img.width;
        let height = img.height;
        if (width > maxSize || height > maxSize) {
          const ratio = Math.min(maxSize / width, maxSize / height);
          width *= ratio;
          height *= ratio;
        }

        const newObj: ImageObject = {
          id: this.generateId(),
          type: 'IMAGE',
          x: 100,
          y: 100,
          width,
          height,
          color: '#000000',
          lineWidth: 1,
          dataUrl,
          imageElement: img
        };
        this.objects.push(newObj);
        this.selectedId = newObj.id;
        this.setTool('SELECT');
        this.renderCanvas();
        this.updateUI();
        this.dispatchChangeEvent();
      };
      img.src = dataUrl;
    };

    reader.readAsDataURL(file);
    input.value = '';
  }

  // 显示图片大小超出限制的错误提示
  private showImageSizeError(currentSizeKB: number, maxSizeKB: number): void {
    const formatSize = (kb: number): string => {
      if (kb >= 1024) {
        return `${(kb / 1024).toFixed(2)} MB`;
      }
      return `${kb.toFixed(2)} KB`;
    };

    const message = `${this.t('imageSizeExceeded')}！\n${this.t('currentImageSize')}: ${formatSize(currentSizeKB)}\n${this.t('imageSizeLimit')}: ${formatSize(maxSizeKB)}`;

    this.showToast(message, 'error');
  }

  // 显示 Toast 提示
  private showToast(message: string, type: 'error' | 'success' | 'warning' | 'info' = 'info'): void {
    // 移除已存在的 toast
    const existingToast = this.shadow.querySelector('.editor-toast');
    if (existingToast) {
      existingToast.remove();
    }

    const toast = document.createElement('div');
    toast.className = `editor-toast editor-toast-${type}`;
    toast.innerHTML = `
      <div class="toast-icon">
        ${type === 'error' ? `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
          <circle cx="12" cy="12" r="10"/>
          <line x1="15" y1="9" x2="9" y2="15"/>
          <line x1="9" y1="9" x2="15" y2="15"/>
        </svg>` : ''}
        ${type === 'success' ? `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
          <circle cx="12" cy="12" r="10"/>
          <path d="M9 12l2 2 4-4"/>
        </svg>` : ''}
        ${type === 'warning' ? `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
          <path d="M10.29 3.86L1.82 18a2 2 0 001.71 3h16.94a2 2 0 001.71-3L13.71 3.86a2 2 0 00-3.42 0z"/>
          <line x1="12" y1="9" x2="12" y2="13"/>
          <line x1="12" y1="17" x2="12.01" y2="17"/>
        </svg>` : ''}
        ${type === 'info' ? `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
          <circle cx="12" cy="12" r="10"/>
          <line x1="12" y1="16" x2="12" y2="12"/>
          <line x1="12" y1="8" x2="12.01" y2="8"/>
        </svg>` : ''}
      </div>
      <div class="toast-message">${message.replace(/\n/g, '<br>')}</div>
      <button class="toast-close">×</button>
    `;

    // 添加样式
    const style = document.createElement('style');
    style.textContent = `
      .editor-toast {
        position: absolute;
        top: 20px;
        left: 50%;
        transform: translateX(-50%);
        display: flex;
        align-items: flex-start;
        gap: 12px;
        padding: 16px 20px;
        border-radius: 8px;
        box-shadow: 0 4px 20px rgba(0, 0, 0, 0.15);
        z-index: 10000;
        animation: toastSlideIn 0.3s ease-out;
        max-width: 400px;
        min-width: 280px;
      }
      .editor-toast-error {
        background: linear-gradient(135deg, #fee2e2 0%, #fecaca 100%);
        border: 1px solid #f87171;
        color: #b91c1c;
      }
      .editor-toast-success {
        background: linear-gradient(135deg, #dcfce7 0%, #bbf7d0 100%);
        border: 1px solid #4ade80;
        color: #15803d;
      }
      .editor-toast-warning {
        background: linear-gradient(135deg, #fef3c7 0%, #fde68a 100%);
        border: 1px solid #fbbf24;
        color: #b45309;
      }
      .editor-toast-info {
        background: linear-gradient(135deg, #dbeafe 0%, #bfdbfe 100%);
        border: 1px solid #60a5fa;
        color: #1d4ed8;
      }
      .toast-icon {
        flex-shrink: 0;
        width: 24px;
        height: 24px;
      }
      .toast-icon svg {
        width: 100%;
        height: 100%;
      }
      .toast-message {
        flex: 1;
        font-size: 14px;
        line-height: 1.5;
        word-break: break-word;
      }
      .toast-close {
        flex-shrink: 0;
        background: none;
        border: none;
        font-size: 20px;
        cursor: pointer;
        opacity: 0.6;
        padding: 0;
        line-height: 1;
        color: inherit;
      }
      .toast-close:hover {
        opacity: 1;
      }
      @keyframes toastSlideIn {
        from {
          opacity: 0;
          transform: translateX(-50%) translateY(-20px);
        }
        to {
          opacity: 1;
          transform: translateX(-50%) translateY(0);
        }
      }
      @keyframes toastSlideOut {
        from {
          opacity: 1;
          transform: translateX(-50%) translateY(0);
        }
        to {
          opacity: 0;
          transform: translateX(-50%) translateY(-20px);
        }
      }
    `;
    toast.appendChild(style);

    // 关闭按钮事件
    const closeBtn = toast.querySelector('.toast-close');
    closeBtn?.addEventListener('click', () => {
      toast.style.animation = 'toastSlideOut 0.3s ease-out forwards';
      setTimeout(() => toast.remove(), 300);
    });

    // 自动关闭
    setTimeout(() => {
      if (toast.parentNode) {
        toast.style.animation = 'toastSlideOut 0.3s ease-out forwards';
        setTimeout(() => toast.remove(), 300);
      }
    }, 5000);

    this.container.appendChild(toast);
  }

  // 保存 JSON
  private saveJson(): void {
    const data = {
      version: '1.0',
      objects: this.objects.map(obj => {
        const { imageElement, ...rest } = obj as ImageObject;
        return rest;
      })
    };
    const json = JSON.stringify(data, null, 2);
    const blob = new Blob([json], { type: 'application/json' });
    const url = URL.createObjectURL(blob);
    const a = document.createElement('a');
    a.href = url;
    a.download = 'canvas-project.json';
    a.click();
    URL.revokeObjectURL(url);
  }

  // 加载 JSON
  private loadJson(e: Event): void {
    const input = e.target as HTMLInputElement;
    if (!input.files || input.files.length === 0) return;

    const file = input.files[0];
    const reader = new FileReader();

    reader.onload = (event) => {
      try {
        const data = JSON.parse(event.target?.result as string);
        if (data.objects && Array.isArray(data.objects)) {
          this.saveHistory();
          this.objects = data.objects;
          this.selectedId = null;

          // 重新加载图片
          this.objects.forEach(obj => {
            if (obj.type === 'IMAGE' && (obj as ImageObject).dataUrl) {
              const img = new Image();
              img.onload = () => {
                (obj as ImageObject).imageElement = img;
                this.renderCanvas();
              };
              img.src = (obj as ImageObject).dataUrl;
            }
          });

          this.renderCanvas();
          this.updateUI();
          this.dispatchChangeEvent();
        }
      } catch (err) {
        console.error('Failed to load JSON:', err);
      }
    };

    reader.readAsText(file);
    input.value = '';
  }

  // 导出 PNG
  private exportPng(): void {
    // 创建临时画布
    const tempCanvas = document.createElement('canvas');
    tempCanvas.width = this.canvas.width;
    tempCanvas.height = this.canvas.height;
    const tempCtx = tempCanvas.getContext('2d')!;

    // 绘制白色背景
    tempCtx.fillStyle = '#ffffff';
    tempCtx.fillRect(0, 0, tempCanvas.width, tempCanvas.height);

    // 应用缩放和平移
    tempCtx.translate(this.panOffset.x, this.panOffset.y);
    tempCtx.scale(this.scale, this.scale);

    // 绘制所有对象
    this.objects.forEach(obj => this.drawObject(tempCtx, obj));

    // 下载
    const url = tempCanvas.toDataURL('image/png');
    const a = document.createElement('a');
    a.href = url;
    a.download = 'canvas-export.png';
    a.click();
  }

  // 清空画布
  private clearCanvas(): void {
    if (this.objects.length === 0) return;

    this.saveHistory();
    this.objects = [];
    this.selectedId = null;
    this.renderCanvas();
    this.dispatchChangeEvent();
    this.updateUI();
  }

  // 更新 UI
  private updateUI(): void {
    // 更新选中状态显示
    const selectionInfo = this.shadow.querySelector('.selection-info');
    if (selectionInfo) {
      if (this.selectedIds.size > 0) {
        // 多选显示
        selectionInfo.innerHTML = `
          <span class="selection-label">${this.t('multiSelected').replace('{count}', String(this.selectedIds.size))}</span>
          <button class="delete-btn" title="${this.t('delete')}">
            <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
              <path d="M3 6h18M19 6v14a2 2 0 01-2 2H7a2 2 0 01-2-2V6m3 0V4a2 2 0 012-2h4a2 2 0 012 2v2"/>
            </svg>
          </button>
        `;
        selectionInfo.classList.add('visible');
        const deleteBtn = selectionInfo.querySelector('.delete-btn');
        if (deleteBtn) {
          deleteBtn.addEventListener('click', () => this.deleteSelected());
        }
      } else if (this.selectedId) {
        const selectedObj = this.objects.find(o => o.id === this.selectedId);
        if (selectedObj) {
          const typeLabels: Record<string, string> = {
            'RECTANGLE': '矩形',
            'CIRCLE': '圆形',
            'PATH': '画笔',
            'TEXT': '文本',
            'IMAGE': '图片'
          };
          const typeLabel = typeLabels[selectedObj.type] || selectedObj.type;
          selectionInfo.innerHTML = `
            <span class="selection-label">${this.t('selected')}: ${typeLabel}</span>
            <button class="delete-btn" title="${this.t('delete')}">
              <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                <path d="M3 6h18M19 6v14a2 2 0 01-2 2H7a2 2 0 01-2-2V6m3 0V4a2 2 0 012-2h4a2 2 0 012 2v2"/>
              </svg>
            </button>
          `;
          selectionInfo.classList.add('visible');
          const deleteBtn = selectionInfo.querySelector('.delete-btn');
          if (deleteBtn) {
            deleteBtn.addEventListener('click', () => this.deleteSelected());
          }
        }
      } else {
        selectionInfo.classList.remove('visible');
        selectionInfo.innerHTML = '';
      }
    }

    // 更新滤镜控制面板显示
    const filterControls = this.shadow.querySelector('.filter-controls') as HTMLElement;
    if (filterControls) {
      const selectedObjs = this.getSelectedObjects();
      const imageObj = selectedObjs.find(o => o.type === 'IMAGE') as ImageObject | undefined;
      if (imageObj) {
        filterControls.style.display = 'flex';
        // 更新滑块值
        const brightnessSlider = filterControls.querySelector('.filter-brightness') as HTMLInputElement;
        const contrastSlider = filterControls.querySelector('.filter-contrast') as HTMLInputElement;
        if (brightnessSlider) {
          const brightness = imageObj.filters?.brightness ?? 100;
          brightnessSlider.value = String(brightness);
          const valueSpan = brightnessSlider.parentElement?.querySelector('.filter-value');
          if (valueSpan) valueSpan.textContent = `${brightness}%`;
        }
        if (contrastSlider) {
          const contrast = imageObj.filters?.contrast ?? 100;
          contrastSlider.value = String(contrast);
          const valueSpan = contrastSlider.parentElement?.querySelector('.filter-value');
          if (valueSpan) valueSpan.textContent = `${contrast}%`;
        }
      } else {
        filterControls.style.display = 'none';
      }
    }

    // 更新撤销按钮状态
    const undoBtn = this.shadow.querySelector('.undo-btn') as HTMLButtonElement;
    if (undoBtn) {
      undoBtn.disabled = this.history.length === 0;
    }

    // 更新重做按钮状态
    const redoBtn = this.shadow.querySelector('.redo-btn') as HTMLButtonElement;
    if (redoBtn) {
      redoBtn.disabled = this.redoHistory.length === 0;
    }

    // 更新空画布提示显示
    this.updateEmptyHint();
  }

  // 更新空画布提示显示状态
  private updateEmptyHint(): void {
    const emptyHint = this.shadow.querySelector('.empty-hint') as HTMLElement;
    if (emptyHint) {
      // 如果有对象或正在绘制，隐藏提示
      const shouldHide = this.objects.length > 0 || this.currentObject !== null;
      emptyHint.style.display = shouldHide ? 'none' : 'flex';
    }
  }

  // 立即隐藏空画布提示（用于开始绘制时）
  private hideEmptyHint(): void {
    const emptyHint = this.shadow.querySelector('.empty-hint') as HTMLElement;
    if (emptyHint) {
      emptyHint.style.display = 'none';
    }
  }

  // ========== 热区功能相关方法 ==========

  // 绑定热区相关事件
  private bindHotzoneEvents(): void {
    // 右键菜单项点击
    this.contextMenu.querySelectorAll('.context-menu-item').forEach(item => {
      item.addEventListener('click', (e) => {
        const action = (e.target as HTMLElement).getAttribute('data-action');
        this.handleHotzoneAction(action);
        this.hideContextMenu();
      });
    });

    // 点击其他地方隐藏右键菜单
    this.shadow.addEventListener('mousedown', (e) => {
      if (!this.contextMenu.contains(e.target as Node)) {
        this.hideContextMenu();
      }
    });

    // 抽屉关闭按钮
    const closeBtn = this.hotzoneDrawer.querySelector('.hotzone-drawer-close');
    closeBtn?.addEventListener('click', () => this.hideHotzoneDrawer());

    // 抽屉取消按钮
    const cancelBtn = this.hotzoneDrawer.querySelector('.hotzone-btn-cancel');
    cancelBtn?.addEventListener('click', () => this.hideHotzoneDrawer());

    // 抽屉保存按钮
    const saveBtn = this.hotzoneDrawer.querySelector('.hotzone-btn-save');
    saveBtn?.addEventListener('click', () => this.saveHotzone());
  }

  // 处理右键菜单
  private handleContextMenu(e: MouseEvent): void {
    e.preventDefault();

    const { x, y } = this.getMousePos(e);
    const clickedObject = [...this.objects].reverse().find(obj => this.isHit(obj, x, y));

    // 只对文本对象显示右键菜单
    if (clickedObject && clickedObject.type === 'TEXT') {
      const textObj = clickedObject as TextObject;
      this.hotzoneEditingTextId = textObj.id;

      // 根据是否有热区显示不同菜单项
      const hasHotzone = !!textObj.hotzone;
      const createItem = this.contextMenu.querySelector('[data-action="hotzone-create"]') as HTMLElement;
      const editItem = this.contextMenu.querySelector('[data-action="hotzone-edit"]') as HTMLElement;
      const removeItem = this.contextMenu.querySelector('[data-action="hotzone-remove"]') as HTMLElement;

      if (hasHotzone) {
        createItem.style.display = 'none';
        editItem.style.display = 'block';
        removeItem.style.display = 'block';
      } else {
        createItem.style.display = 'block';
        editItem.style.display = 'none';
        removeItem.style.display = 'none';
      }

      // 显示右键菜单
      this.contextMenu.style.display = 'block';
      this.contextMenu.style.left = `${e.offsetX}px`;
      this.contextMenu.style.top = `${e.offsetY}px`;
    } else {
      this.hideContextMenu();
    }
  }

  // 隐藏右键菜单
  private hideContextMenu(): void {
    this.contextMenu.style.display = 'none';
  }

  // 处理热区操作
  private handleHotzoneAction(action: string | null): void {
    if (!action || !this.hotzoneEditingTextId) return;

    const textObj = this.objects.find(o => o.id === this.hotzoneEditingTextId) as TextObject | undefined;
    if (!textObj) return;

    switch (action) {
      case 'hotzone-create':
      case 'hotzone-edit':
        this.showHotzoneDrawer(textObj);
        break;
      case 'hotzone-remove':
        this.removeHotzone(textObj);
        break;
    }
  }

  // 显示热区配置抽屉
  private showHotzoneDrawer(textObj: TextObject): void {
    // 填充表单
    const variableNameInput = this.hotzoneDrawer.querySelector('input[name="variableName"]') as HTMLInputElement;
    const defaultValueInput = this.hotzoneDrawer.querySelector('input[name="defaultValue"]') as HTMLInputElement;
    const descriptionInput = this.hotzoneDrawer.querySelector('textarea[name="description"]') as HTMLTextAreaElement;

    if (textObj.hotzone) {
      variableNameInput.value = textObj.hotzone.variableName || '';
      defaultValueInput.value = textObj.hotzone.defaultValue || '';
      descriptionInput.value = textObj.hotzone.description || '';
    } else {
      variableNameInput.value = '';
      defaultValueInput.value = '';
      descriptionInput.value = '';
    }

    this.hotzoneDrawer.style.display = 'flex';
  }

  // 隐藏热区配置抽屉
  private hideHotzoneDrawer(): void {
    this.hotzoneDrawer.style.display = 'none';
    this.hotzoneEditingTextId = null;
  }

  // 保存热区配置
  private saveHotzone(): void {
    if (!this.hotzoneEditingTextId) return;

    const textObj = this.objects.find(o => o.id === this.hotzoneEditingTextId) as TextObject | undefined;
    if (!textObj) return;

    const variableNameInput = this.hotzoneDrawer.querySelector('input[name="variableName"]') as HTMLInputElement;
    const defaultValueInput = this.hotzoneDrawer.querySelector('input[name="defaultValue"]') as HTMLInputElement;
    const descriptionInput = this.hotzoneDrawer.querySelector('textarea[name="description"]') as HTMLTextAreaElement;

    const variableName = variableNameInput.value.trim();
    if (!variableName) {
      variableNameInput.focus();
      return;
    }

    this.saveHistory();

    textObj.hotzone = {
      variableName,
      defaultValue: defaultValueInput.value.trim() || undefined,
      description: descriptionInput.value.trim() || undefined,
    };

    this.hideHotzoneDrawer();
    this.renderCanvas();
    this.dispatchChangeEvent();
  }

  // 移除热区
  private removeHotzone(textObj: TextObject): void {
    this.saveHistory();
    delete textObj.hotzone;
    this.renderCanvas();
    this.dispatchChangeEvent();
  }

  // 应用热区数据（替换文本内容）
  private applyHotzoneData(): void {
    if (Object.keys(this.hotzoneData).length === 0) return;

    this.objects.forEach(obj => {
      if (obj.type === 'TEXT') {
        const textObj = obj as TextObject;
        if (textObj.hotzone && textObj.hotzone.variableName) {
          const value = this.hotzoneData[textObj.hotzone.variableName];
          if (value !== undefined) {
            textObj.text = value;
          } else if (textObj.hotzone.defaultValue) {
            textObj.text = textObj.hotzone.defaultValue;
          }
        }
      }
    });
  }

  // 渲染 DOM 结构
  private render(): void {
    const themeColor = this.config.themeColor || DEFAULT_THEME_COLOR;
    const tool = this.getToolConfig();

    this.shadow.innerHTML = `
      <style>${this.getStyles()}</style>
      <div class="editor-container">
        <!-- 左侧工具栏 -->
        <div class="toolbar">
          ${this.createToolButton('SELECT', 'select-icon', this.t('select'))}
          <div class="divider"></div>

          <!-- 形状工具组 -->
          ${(tool.rectangle || tool.circle || tool.line || tool.arrow || tool.polygon || tool.triangle || tool.star || tool.heart || tool.diamond) ? `
            <div class="tool-group" data-group="shapes">
              <button class="tool-btn tool-group-btn" title="${this.t('shapes')}">
                <svg class="icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                  <rect x="3" y="3" width="18" height="18" rx="2"/>
                </svg>
                <span class="dropdown-indicator">▾</span>
              </button>
              <div class="tool-dropdown">
                ${tool.rectangle ? `
                  <button class="tool-btn dropdown-item" data-tool="RECTANGLE" title="${this.t('rectangle')}">
                    <svg class="icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                      <rect x="3" y="3" width="18" height="18" rx="2"/>
                    </svg>
                    <span class="dropdown-label">${this.t('rectangle')}</span>
                  </button>
                ` : ''}
                ${tool.circle ? `
                  <button class="tool-btn dropdown-item" data-tool="CIRCLE" title="${this.t('circle')}">
                    <svg class="icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                      <circle cx="12" cy="12" r="10"/>
                    </svg>
                    <span class="dropdown-label">${this.t('circle')}</span>
                  </button>
                ` : ''}
                ${tool.triangle ? `
                  <button class="tool-btn dropdown-item" data-tool="TRIANGLE" title="${this.t('triangle')}">
                    <svg class="icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                      <polygon points="12 3 22 21 2 21"/>
                    </svg>
                    <span class="dropdown-label">${this.t('triangle')}</span>
                  </button>
                ` : ''}
                ${tool.star ? `
                  <button class="tool-btn dropdown-item" data-tool="STAR" title="${this.t('star')}">
                    <svg class="icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                      <polygon points="12 2 15.09 8.26 22 9.27 17 14.14 18.18 21.02 12 17.77 5.82 21.02 7 14.14 2 9.27 8.91 8.26 12 2"/>
                    </svg>
                    <span class="dropdown-label">${this.t('star')}</span>
                  </button>
                ` : ''}
                ${tool.heart ? `
                  <button class="tool-btn dropdown-item" data-tool="HEART" title="${this.t('heart')}">
                    <svg class="icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                      <path d="M20.84 4.61a5.5 5.5 0 0 0-7.78 0L12 5.67l-1.06-1.06a5.5 5.5 0 0 0-7.78 7.78l1.06 1.06L12 21.23l7.78-7.78 1.06-1.06a5.5 5.5 0 0 0 0-7.78z"/>
                    </svg>
                    <span class="dropdown-label">${this.t('heart')}</span>
                  </button>
                ` : ''}
                ${tool.diamond ? `
                  <button class="tool-btn dropdown-item" data-tool="DIAMOND" title="${this.t('diamond')}">
                    <svg class="icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                      <polygon points="12 2 22 12 12 22 2 12"/>
                    </svg>
                    <span class="dropdown-label">${this.t('diamond')}</span>
                  </button>
                ` : ''}
                ${tool.bezier ? `
                  <button class="tool-btn dropdown-item" data-tool="BEZIER" title="${this.t('bezierTool')}">
                    <svg class="icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                      <path d="M3 17c3-6 9-6 12 0s9 6 12 0"/>
                      <circle cx="3" cy="17" r="2"/>
                      <circle cx="21" cy="17" r="2"/>
                    </svg>
                    <span class="dropdown-label">${this.t('bezier')}</span>
                  </button>
                ` : ''}
                ${tool.smoothCurve !== false ? `
                  <button class="tool-btn dropdown-item" data-tool="SMOOTH_CURVE" title="${this.t('smoothCurveTool')}">
                    <svg class="icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                      <path d="M3 20c3-6 6-9 9-9s6 3 9 9"/>
                      <circle cx="3" cy="20" r="1.5"/>
                      <circle cx="12" cy="11" r="1.5"/>
                      <circle cx="21" cy="20" r="1.5"/>
                    </svg>
                    <span class="dropdown-label">${this.t('smoothCurve')}</span>
                  </button>
                ` : ''}
                ${tool.line ? `
                  <button class="tool-btn dropdown-item" data-tool="LINE" title="${this.t('line')}">
                    <svg class="icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                      <line x1="5" y1="19" x2="19" y2="5"/>
                    </svg>
                    <span class="dropdown-label">${this.t('line')}</span>
                  </button>
                ` : ''}
                ${tool.arrow ? `
                  <button class="tool-btn dropdown-item" data-tool="ARROW" title="${this.t('arrow')}">
                    <svg class="icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                      <line x1="5" y1="19" x2="19" y2="5"/>
                      <polyline points="9 5 19 5 19 15"/>
                    </svg>
                    <span class="dropdown-label">${this.t('arrow')}</span>
                  </button>
                ` : ''}
                ${tool.arrow ? `
                  <button class="tool-btn dropdown-item" data-tool="DOUBLE_ARROW" title="${this.t('doubleArrow')}">
                    <svg class="icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                      <line x1="5" y1="19" x2="19" y2="5"/>
                      <polyline points="9 5 19 5 19 15"/>
                      <polyline points="15 19 5 19 5 9"/>
                    </svg>
                    <span class="dropdown-label">${this.t('doubleArrow')}</span>
                  </button>
                ` : ''}
                ${tool.polygon ? `
                  <button class="tool-btn dropdown-item" data-tool="POLYGON" title="${this.t('polygon')}">
                    <svg class="icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                      <polygon points="12 2 22 8.5 22 15.5 12 22 2 15.5 2 8.5 12 2"/>
                    </svg>
                    <span class="dropdown-label">${this.t('polygon')}</span>
                  </button>
                ` : ''}
              </div>
            </div>
          ` : ''}

          <!-- 画笔单独 -->
          ${tool.pencil ? this.createToolButton('PENCIL', 'pencil-icon', this.t('pencil')) : ''}

          <!-- 文本工具组 -->
          ${tool.text ? `
            <div class="tool-group" data-group="text">
              <button class="tool-btn tool-group-btn" title="${this.t('text') || '文本'}">
                <svg class="icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                  <polyline points="4 7 4 4 20 4 20 7"/>
                  <line x1="9" y1="20" x2="15" y2="20"/>
                  <line x1="12" y1="4" x2="12" y2="20"/>
                </svg>
                <span class="dropdown-indicator">▾</span>
              </button>
              <div class="tool-dropdown">
                <button class="tool-btn dropdown-item" data-tool="TEXT" title="${this.t('text') || '普通文本'}">
                  <svg class="icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                    <polyline points="4 7 4 4 20 4 20 7"/>
                    <line x1="9" y1="20" x2="15" y2="20"/>
                    <line x1="12" y1="4" x2="12" y2="20"/>
                  </svg>
                  <span class="dropdown-label">${this.t('text') || '普通文本'}</span>
                </button>
                <button class="tool-btn dropdown-item" data-tool="RICH_TEXT" title="${this.t('richText') || '富文本'}">
                  <svg class="icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                    <path d="M4 7V4h16v3"/>
                    <path d="M9 20h6"/>
                    <path d="M12 4v16"/>
                    <path d="M7 12h4" stroke-width="3"/>
                  </svg>
                  <span class="dropdown-label">${this.t('richText') || '富文本'}</span>
                </button>
              </div>
            </div>
          ` : ''}

          <!-- 媒体工具组（图片+图层） -->
          ${(tool.image || tool.layers) ? `
            <div class="tool-group" data-group="media">
              <button class="tool-btn tool-group-btn" title="${this.t('media')}">
                <svg class="icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                  <rect x="3" y="3" width="18" height="18" rx="2"/>
                  <circle cx="8.5" cy="8.5" r="1.5"/>
                  <path d="M21 15l-5-5L5 21"/>
                </svg>
                <span class="dropdown-indicator">▾</span>
              </button>
              <div class="tool-dropdown">
                ${tool.image ? `
                  <label class="tool-btn dropdown-item" title="${this.t('insertImage')}">
                    <svg class="icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                      <rect x="3" y="3" width="18" height="18" rx="2"/>
                      <circle cx="8.5" cy="8.5" r="1.5"/>
                      <path d="M21 15l-5-5L5 21"/>
                    </svg>
                    <span class="dropdown-label">${this.t('insertImage')}</span>
                    <input type="file" accept="image/*" class="hidden image-input" />
                  </label>
                ` : ''}
                ${tool.layers ? `
                  <button class="tool-btn dropdown-item layers-btn" title="${this.t('layers')}">
                    <svg class="icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                      <polygon points="12 2 2 7 12 12 22 7 12 2"/>
                      <polyline points="2 17 12 22 22 17"/>
                      <polyline points="2 12 12 17 22 12"/>
                    </svg>
                    <span class="dropdown-label">${this.t('layers')}</span>
                  </button>
                ` : ''}
              </div>
            </div>
          ` : ''}

          <!-- 组合/解组工具组 -->
          ${tool.group ? `
            <div class="tool-group" data-group="group">
              <button class="tool-btn tool-group-btn group-tool-btn" title="${this.t('group')}">
                <svg class="icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                  <rect x="3" y="3" width="7" height="7" rx="1"/>
                  <rect x="14" y="3" width="7" height="7" rx="1"/>
                  <rect x="3" y="14" width="7" height="7" rx="1"/>
                  <rect x="14" y="14" width="7" height="7" rx="1"/>
                </svg>
                <span class="dropdown-indicator">▾</span>
              </button>
              <div class="tool-dropdown">
                <button class="tool-btn dropdown-item group-btn" title="${this.t('group')}">
                  <svg class="icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                    <rect x="3" y="3" width="7" height="7" rx="1"/>
                    <rect x="14" y="3" width="7" height="7" rx="1"/>
                    <rect x="3" y="14" width="7" height="7" rx="1"/>
                    <rect x="14" y="14" width="7" height="7" rx="1"/>
                    <path d="M10 12h4M12 10v4"/>
                  </svg>
                  <span class="dropdown-label">${this.t('group')}</span>
                </button>
                <button class="tool-btn dropdown-item ungroup-btn" title="${this.t('ungroup')}">
                  <svg class="icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                    <rect x="3" y="3" width="7" height="7" rx="1"/>
                    <rect x="14" y="3" width="7" height="7" rx="1"/>
                    <rect x="3" y="14" width="7" height="7" rx="1"/>
                    <rect x="14" y="14" width="7" height="7" rx="1"/>
                    <path d="M9 12h6"/>
                  </svg>
                  <span class="dropdown-label">${this.t('ungroup')}</span>
                </button>
              </div>
            </div>
          ` : ''}

          <!-- 形状库工具组 -->
          ${tool.shapePanel ? `
            <div class="tool-group shape-library-group" data-group="shape-library">
              <button class="tool-btn tool-group-btn shape-library-btn" title="${this.t('shapePanel')}">
                <svg class="icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                  <rect x="3" y="3" width="7" height="7" rx="1"/>
                  <circle cx="17.5" cy="6.5" r="3.5"/>
                  <polygon points="6.5 14 10 21 3 21"/>
                  <path d="M14 14h7v7h-7z"/>
                </svg>
                <span class="dropdown-indicator">▾</span>
              </button>
              <div class="tool-dropdown shape-library-dropdown">
                <div class="shape-library-header">
                  <span>${this.t('shapePanel')}</span>
                  <input type="text" class="shape-library-search" placeholder="${this.t('search') || '搜索...'}" />
                </div>
                <div class="shape-library-grid"></div>
                <div class="shape-library-empty">${this.t('noShapes')}</div>
              </div>
            </div>
          ` : ''}

          <div class="divider"></div>
          ${tool.undo !== false ? `
            <button class="tool-btn undo-btn" title="${this.t('undo')}" disabled>
              <svg class="icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                <path d="M1 4v6h6"/>
                <path d="M3.51 15a9 9 0 1 0 2.13-9.36L1 10"/>
              </svg>
            </button>
          ` : ''}
          ${tool.redo ? `
            <button class="tool-btn redo-btn" title="${this.t('redo')}" disabled>
              <svg class="icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                <path d="M23 4v6h-6"/>
                <path d="M20.49 15a9 9 0 1 1-2.12-9.36L23 10"/>
              </svg>
            </button>
          ` : ''}
          <div class="spacer"></div>

          ${tool.color ? `
            <input type="color" class="color-picker" value="${this.color}" title="${this.t('colorPicker')}" />
          ` : ''}
        </div>

        <!-- 主区域 -->
        <div class="main-area">
          <!-- 顶部栏 -->
          <div class="top-bar">
            <div class="top-bar-left">
              <h2 class="title">${this.config.title}</h2>
              <div class="selection-info"></div>
            </div>
            <div class="top-bar-right">
              <!-- 滤镜控制面板（选中图片时显示） -->
              <div class="filter-controls" style="display: none;">
                <span class="filter-label">${this.t('filters')}:</span>
                <div class="filter-item">
                  <label>${this.t('brightness')}</label>
                  <input type="range" class="filter-slider filter-brightness" min="0" max="200" value="100" />
                  <span class="filter-value">100%</span>
                </div>
                <div class="filter-item">
                  <label>${this.t('contrast')}</label>
                  <input type="range" class="filter-slider filter-contrast" min="0" max="200" value="100" />
                  <span class="filter-value">100%</span>
                </div>
                <button class="filter-reset-btn" title="${this.t('resetFilters')}">
                  <svg class="icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                    <path d="M3 12a9 9 0 1 0 9-9 9.75 9.75 0 0 0-6.74 2.74L3 8"/>
                    <path d="M3 3v5h5"/>
                  </svg>
                </button>
              </div>
              <!-- 填充模式选择器 -->
              <div class="fill-mode-group">
                <button class="tool-btn fill-mode-btn ${this.fillMode === 'stroke' ? 'active' : ''}" data-fill-mode="stroke" title="${this.t('fillModeStroke')}">
                  <svg class="icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                    <rect x="4" y="4" width="16" height="16" rx="2"/>
                  </svg>
                </button>
                <button class="tool-btn fill-mode-btn ${this.fillMode === 'fill' ? 'active' : ''}" data-fill-mode="fill" title="${this.t('fillModeFill')}">
                  <svg class="icon" viewBox="0 0 24 24" fill="currentColor" stroke="none">
                    <rect x="4" y="4" width="16" height="16" rx="2"/>
                  </svg>
                </button>
                <button class="tool-btn fill-mode-btn ${this.fillMode === 'both' ? 'active' : ''}" data-fill-mode="both" title="${this.t('fillModeBoth')}">
                  <svg class="icon" viewBox="0 0 24 24" fill="currentColor" stroke="currentColor" stroke-width="2">
                    <rect x="4" y="4" width="16" height="16" rx="2"/>
                  </svg>
                </button>
              </div>
              <!-- 线条样式选择器 -->
              <div class="line-style-group">
                <button class="tool-btn line-style-btn ${this.lineStyle === 'solid' ? 'active' : ''}" data-line-style="solid" title="${this.t('lineStyleSolid')}">
                  <svg class="icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                    <line x1="3" y1="12" x2="21" y2="12"/>
                  </svg>
                </button>
                <button class="tool-btn line-style-btn ${this.lineStyle === 'dashed' ? 'active' : ''}" data-line-style="dashed" title="${this.t('lineStyleDashed')}">
                  <svg class="icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-dasharray="4 2">
                    <line x1="3" y1="12" x2="21" y2="12"/>
                  </svg>
                </button>
                <button class="tool-btn line-style-btn ${this.lineStyle === 'dotted' ? 'active' : ''}" data-line-style="dotted" title="${this.t('lineStyleDotted')}">
                  <svg class="icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-dasharray="2 2">
                    <line x1="3" y1="12" x2="21" y2="12"/>
                  </svg>
                </button>
              </div>
              ${tool.align ? `
                <div class="align-controls">
                  <button class="align-btn align-left-btn" title="${this.t('alignLeft')}">
                    <svg class="icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                      <line x1="17" y1="10" x2="3" y2="10"/><line x1="21" y1="6" x2="3" y2="6"/>
                      <line x1="21" y1="14" x2="3" y2="14"/><line x1="17" y1="18" x2="3" y2="18"/>
                    </svg>
                  </button>
                  <button class="align-btn align-center-btn" title="${this.t('alignCenter')}">
                    <svg class="icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                      <line x1="18" y1="10" x2="6" y2="10"/><line x1="21" y1="6" x2="3" y2="6"/>
                      <line x1="21" y1="14" x2="3" y2="14"/><line x1="18" y1="18" x2="6" y2="18"/>
                    </svg>
                  </button>
                  <button class="align-btn align-right-btn" title="${this.t('alignRight')}">
                    <svg class="icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                      <line x1="21" y1="10" x2="7" y2="10"/><line x1="21" y1="6" x2="3" y2="6"/>
                      <line x1="21" y1="14" x2="3" y2="14"/><line x1="21" y1="18" x2="7" y2="18"/>
                    </svg>
                  </button>
                </div>
              ` : ''}
              ${tool.zoom ? `
                <div class="zoom-controls">
                  <button class="zoom-btn zoom-out-btn" title="${this.t('zoomOut')}">
                    <svg class="icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                      <circle cx="11" cy="11" r="8"/><path d="M21 21l-4.35-4.35M8 11h6"/>
                    </svg>
                  </button>
                  <button class="zoom-text" title="${this.t('resetZoom')}">100%</button>
                  <button class="zoom-btn zoom-in-btn" title="${this.t('zoomIn')}">
                    <svg class="icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                      <circle cx="11" cy="11" r="8"/><path d="M21 21l-4.35-4.35M11 8v6M8 11h6"/>
                    </svg>
                  </button>
                </div>
              ` : ''}
              ${(tool.exportJson || tool.importJson || tool.download || tool.clear) ? `
                <div class="file-controls">
                  ${tool.exportJson ? `
                    <button class="file-btn save-json-btn" title="${this.t('saveProject')}">
                      <svg class="icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                        <path d="M19 21H5a2 2 0 01-2-2V5a2 2 0 012-2h11l5 5v11a2 2 0 01-2 2z"/>
                        <polyline points="17 21 17 13 7 13 7 21"/><polyline points="7 3 7 8 15 8"/>
                      </svg>
                    </button>
                  ` : ''}
                  ${tool.importJson ? `
                    <label class="file-btn" title="${this.t('loadProject')}">
                      <svg class="icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                        <path d="M22 19a2 2 0 01-2 2H4a2 2 0 01-2-2V5a2 2 0 012-2h5l2 3h9a2 2 0 012 2z"/>
                      </svg>
                      <input type="file" accept=".json" class="hidden load-json-input" />
                    </label>
                  ` : ''}
                  ${tool.download ? `
                    <button class="file-btn export-png-btn" title="${this.t('exportPng')}">
                      <svg class="icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                        <path d="M21 15v4a2 2 0 01-2 2H5a2 2 0 01-2-2v-4M7 10l5 5 5-5M12 15V3"/>
                      </svg>
                    </button>
                  ` : ''}
                  ${tool.clear ? `
                    <div class="clear-btn-wrapper">
                      <button class="file-btn clear-canvas-btn" title="${this.t('clearCanvas')}">
                        <svg class="icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                          <path d="M3 6h18M19 6v14a2 2 0 01-2 2H7a2 2 0 01-2-2V6m3 0V4a2 2 0 012-2h4a2 2 0 012 2v2"/>
                          <line x1="10" y1="11" x2="10" y2="17"/><line x1="14" y1="11" x2="14" y2="17"/>
                        </svg>
                      </button>
                      <div class="clear-confirm-popup">
                        <p class="clear-confirm-text">${this.t('clearConfirm')}</p>
                        <div class="clear-confirm-actions">
                          <button class="clear-confirm-yes">${this.t('confirm')}</button>
                          <button class="clear-confirm-no">${this.t('cancel')}</button>
                        </div>
                      </div>
                    </div>
                  ` : ''}
                </div>
              ` : ''}
            </div>
          </div>

          <!-- 画布容器 -->
          <div class="canvas-container">
            <canvas class="main-canvas"></canvas>

            <!-- 文本输入 -->
            <div class="text-input-container" style="display: none;">
              <div class="text-input-hint">${this.t('textInputHint')}</div>
              <input type="text" class="text-input" placeholder="${this.t('textPlaceholder')}" />
            </div>

            <!-- 富文本编辑器 -->
            <div class="rich-text-editor" style="display: none;">
              <div class="rich-text-toolbar">
                <button class="rich-text-btn" data-action="bold" title="${this.t('bold') || '粗体'}">
                  <svg viewBox="0 0 24 24" width="16" height="16" fill="currentColor">
                    <path d="M15.6 10.79c.97-.67 1.65-1.77 1.65-2.79 0-2.26-1.75-4-4-4H7v14h7.04c2.09 0 3.71-1.7 3.71-3.79 0-1.52-.86-2.82-2.15-3.42zM10 6.5h3c.83 0 1.5.67 1.5 1.5s-.67 1.5-1.5 1.5h-3v-3zm3.5 9H10v-3h3.5c.83 0 1.5.67 1.5 1.5s-.67 1.5-1.5 1.5z"/>
                  </svg>
                </button>
                <button class="rich-text-btn" data-action="italic" title="${this.t('italic') || '斜体'}">
                  <svg viewBox="0 0 24 24" width="16" height="16" fill="currentColor">
                    <path d="M10 4v3h2.21l-3.42 8H6v3h8v-3h-2.21l3.42-8H18V4z"/>
                  </svg>
                </button>
                <div class="rich-text-divider"></div>
                <input type="color" class="rich-text-color" value="#000000" title="${this.t('textColor') || '文字颜色'}">
                <input type="number" class="rich-text-fontsize" value="16" min="8" max="200" title="${this.t('fontSize') || '字号'}">
                <div class="rich-text-divider"></div>
                <button class="rich-text-btn" data-action="add-segment" title="${this.t('addSegment') || '添加文本段'}">
                  <svg viewBox="0 0 24 24" width="16" height="16" fill="currentColor">
                    <path d="M19 13h-6v6h-2v-6H5v-2h6V5h2v6h6v2z"/>
                  </svg>
                </button>
              </div>
              <div class="rich-text-segments"></div>
              <div class="rich-text-actions">
                <button class="rich-text-action-btn cancel">${this.t('cancel') || '取消'}</button>
                <button class="rich-text-action-btn confirm">${this.t('confirm') || '确定'}</button>
              </div>
            </div>

            <!-- 动画时间线编辑器 -->
            <div class="timeline-editor" style="display: none;">
              <div class="timeline-header">
                <span class="timeline-title">${this.t('animationTimeline') || '动画时间线'}</span>
                <div class="timeline-controls">
                  <button class="timeline-btn play" title="${this.t('play') || '播放'}">▶</button>
                  <button class="timeline-btn pause" title="${this.t('pause') || '暂停'}">⏸</button>
                  <button class="timeline-btn stop" title="${this.t('stop') || '停止'}">⏹</button>
                </div>
                <button class="timeline-close" title="${this.t('close') || '关闭'}">×</button>
              </div>
              <div class="timeline-content">
                <div class="timeline-tracks"></div>
                <div class="timeline-ruler">
                  <div class="timeline-playhead"></div>
                </div>
              </div>
              <div class="timeline-footer">
                <button class="timeline-add-keyframe">${this.t('addKeyframe') || '添加关键帧'}</button>
                <span class="timeline-time">0.00s</span>
              </div>
            </div>

            <!-- 空画布提示 -->
            <div class="empty-hint" style="display: ${this.getAttribute('initial-data') ? 'none' : 'flex'};">
              <h3>${this.t('startCreating')}</h3>
              <p>${this.t('selectToolHint')}</p>
            </div>

            <!-- 右键菜单 -->
            <div class="context-menu" style="display: none;">
              <div class="context-menu-item" data-action="hotzone-create">${this.t('hotzoneCreate')}</div>
              <div class="context-menu-item" data-action="hotzone-edit" style="display: none;">${this.t('hotzoneEdit')}</div>
              <div class="context-menu-item context-menu-item-danger" data-action="hotzone-remove" style="display: none;">${this.t('hotzoneRemove')}</div>
            </div>
          </div>

          <!-- 图层面板 -->
          ${tool.layers ? `
            <div class="layer-panel" style="display: none;">
              <div class="layer-panel-header">
                <span>${this.t('layers')}</span>
                <button class="layer-panel-close">&times;</button>
              </div>
              <div class="layer-panel-list"></div>
            </div>
          ` : ''}
        </div>

        <!-- 热区配置抽屉 -->
        <div class="hotzone-drawer" style="display: none;">
          <div class="hotzone-drawer-header">
            <h3>${this.t('hotzoneTitle')}</h3>
            <button class="hotzone-drawer-close">&times;</button>
          </div>
          <div class="hotzone-drawer-body">
            <div class="hotzone-form-group">
              <label>${this.t('hotzoneVariableName')} <span class="required">*</span></label>
              <input type="text" class="hotzone-input" name="variableName" placeholder="${this.t('hotzoneVariableNamePlaceholder')}" />
            </div>
            <div class="hotzone-form-group">
              <label>${this.t('hotzoneDefaultValue')}</label>
              <input type="text" class="hotzone-input" name="defaultValue" placeholder="${this.t('hotzoneDefaultValuePlaceholder')}" />
            </div>
            <div class="hotzone-form-group">
              <label>${this.t('hotzoneDescription')}</label>
              <textarea class="hotzone-textarea" name="description" placeholder="${this.t('hotzoneDescriptionPlaceholder')}"></textarea>
            </div>
          </div>
          <div class="hotzone-drawer-footer">
            <button class="hotzone-btn hotzone-btn-cancel">${this.t('hotzoneCancel')}</button>
            <button class="hotzone-btn hotzone-btn-save">${this.t('hotzoneSave')}</button>
          </div>
        </div>
      </div>
    `;

    // 获取 DOM 引用
    this.container = this.shadow.querySelector('.editor-container')!;
    this.toolbar = this.shadow.querySelector('.toolbar')!;
    this.topBar = this.shadow.querySelector('.top-bar')!;
    this.canvasContainer = this.shadow.querySelector('.canvas-container')!;
    this.canvas = this.shadow.querySelector('.main-canvas')!;
    this.ctx = this.canvas.getContext('2d')!;

    this.textInputContainer = this.shadow.querySelector('.text-input-container')!;
    this.textInput = this.shadow.querySelector('.text-input')!;
    this.richTextEditor = this.shadow.querySelector('.rich-text-editor')!;
    this.timelineEditor = this.shadow.querySelector('.timeline-editor')!;

    // 热区相关 DOM 引用
    this.contextMenu = this.shadow.querySelector('.context-menu')!;
    this.hotzoneDrawer = this.shadow.querySelector('.hotzone-drawer')!;

    // 形状面板 DOM 引用
    this.shapePanelElement = this.shadow.querySelector('.shape-panel')!;

    // 绑定事件
    this.bindEvents();
    this.bindRichTextEditorEvents();
    this.bindTimelineEditorEvents();
    this.bindShapePanelEvents();
  }

  // 绑定事件
  private bindEvents(): void {
    // 画布事件 - 鼠标
    this.canvas.addEventListener('mousedown', (e) => this.handleCanvasPointerDown(e));
    this.canvas.addEventListener('mousemove', (e) => this.handleCanvasPointerMove(e));
    this.canvas.addEventListener('mouseup', () => this.handleCanvasPointerUp());
    this.canvas.addEventListener('mouseleave', () => this.handleCanvasPointerUp());
    this.canvas.addEventListener('dblclick', (e) => this.handleCanvasDoubleClick(e));

    // 画布事件 - 触摸（增强版多点触摸支持）
    this.canvas.addEventListener('touchstart', (e) => this.handleTouchStart(e), { passive: false });
    this.canvas.addEventListener('touchmove', (e) => this.handleTouchMove(e), { passive: false });
    this.canvas.addEventListener('touchend', (e) => this.handleTouchEnd(e), { passive: false });
    this.canvas.addEventListener('touchcancel', (e) => this.handleTouchEnd(e), { passive: false });
    this.canvas.addEventListener('wheel', this.boundHandleWheel, { passive: false });

    // 右键菜单事件
    this.canvas.addEventListener('contextmenu', (e) => {
      // 贝塞尔曲线工具禁用默认右键菜单
      if (this.tool === 'BEZIER') {
        e.preventDefault();
        return;
      }
      // 仅在启用热区时处理右键菜单
      if (this.config.enableHotzone) {
        this.handleContextMenu(e);
      }
    });
    if (this.config.enableHotzone) {
      this.bindHotzoneEvents();
    }

    // 工具按钮
    this.shadow.querySelectorAll('.tool-btn[data-tool]').forEach(btn => {
      btn.addEventListener('mousedown', (e) => {
        // 阻止 blur 事件触发，在点击后手动处理
        e.preventDefault();
      });
      btn.addEventListener('click', () => {
        // 如果有文本输入，先提交
        if (this.isTextInputVisible) {
          this.submitText();
        }
        const tool = btn.getAttribute('data-tool') as ToolType;
        this.setTool(tool);
      });
    });

    // 撤销按钮
    const undoBtn = this.shadow.querySelector('.undo-btn');
    if (undoBtn) {
      undoBtn.addEventListener('click', () => this.undo());
    }

    // 重做按钮
    const redoBtn = this.shadow.querySelector('.redo-btn');
    if (redoBtn) {
      redoBtn.addEventListener('click', () => this.redo());
    }

    // 图层按钮
    const layersBtn = this.shadow.querySelector('.layers-btn');
    if (layersBtn) {
      layersBtn.addEventListener('click', () => this.toggleLayerPanel());
    }

    // 图层面板关闭按钮
    const layerPanelClose = this.shadow.querySelector('.layer-panel-close');
    if (layerPanelClose) {
      layerPanelClose.addEventListener('click', () => this.toggleLayerPanel());
    }

    // 组合按钮
    const groupBtn = this.shadow.querySelector('.group-btn');
    if (groupBtn) {
      groupBtn.addEventListener('click', () => this.groupSelected());
    }

    // 解组按钮
    const ungroupBtn = this.shadow.querySelector('.ungroup-btn');
    if (ungroupBtn) {
      ungroupBtn.addEventListener('click', () => this.ungroupSelected());
    }

    // 对齐按钮
    const alignLeftBtn = this.shadow.querySelector('.align-left-btn');
    const alignCenterBtn = this.shadow.querySelector('.align-center-btn');
    const alignRightBtn = this.shadow.querySelector('.align-right-btn');
    if (alignLeftBtn) alignLeftBtn.addEventListener('click', () => this.alignLeft());
    if (alignCenterBtn) alignCenterBtn.addEventListener('click', () => this.alignCenterH());
    if (alignRightBtn) alignRightBtn.addEventListener('click', () => this.alignRight());

    // 填充模式选择器
    this.shadow.querySelectorAll('.fill-mode-btn').forEach(btn => {
      btn.addEventListener('click', (e) => {
        const target = e.currentTarget as HTMLElement;
        const mode = target.dataset.fillMode as FillMode;
        this.fillMode = mode;
        // 更新按钮状态
        this.shadow.querySelectorAll('.fill-mode-btn').forEach(b => b.classList.remove('active'));
        target.classList.add('active');
        // 如果有选中的形状对象，更新其填充模式
        const selectedObjs = this.getSelectedObjects();
        if (selectedObjs.length > 0) {
          this.saveHistory();
          selectedObjs.forEach(obj => {
            // 支持所有形状的填充模式（除了 LINE、ARROW、BEZIER、PATH、TEXT、IMAGE）
            if (obj.type === 'RECTANGLE') {
              (obj as RectObject).fillMode = mode;
            } else if (obj.type === 'CIRCLE') {
              (obj as CircleObject).fillMode = mode;
            } else if (obj.type === 'POLYGON') {
              (obj as PolygonObject).fillMode = mode;
            } else if (obj.type === 'TRIANGLE') {
              (obj as TriangleObject).fillMode = mode;
            } else if (obj.type === 'STAR') {
              (obj as StarObject).fillMode = mode;
            } else if (obj.type === 'HEART') {
              (obj as HeartObject).fillMode = mode;
            } else if (obj.type === 'DIAMOND') {
              (obj as DiamondObject).fillMode = mode;
            }
          });
          this.renderCanvas();
          this.dispatchChangeEvent();
        }
      });
    });

    // 线条样式选择器
    this.shadow.querySelectorAll('.line-style-btn').forEach(btn => {
      btn.addEventListener('click', (e) => {
        const target = e.currentTarget as HTMLElement;
        const style = target.dataset.lineStyle as LineStyle;
        this.lineStyle = style;
        // 更新按钮状态
        this.shadow.querySelectorAll('.line-style-btn').forEach(b => b.classList.remove('active'));
        target.classList.add('active');
        // 如果有选中的形状对象，更新其样式（除了 BEZIER 和 PATH）
        const selectedObjs = this.getSelectedObjects();
        if (selectedObjs.length > 0) {
          this.saveHistory();
          selectedObjs.forEach(obj => {
            // 支持所有形状的线条样式（除了 BEZIER、PATH、TEXT、IMAGE）
            if (obj.type === 'LINE' || obj.type === 'ARROW') {
              (obj as LineObject | ArrowObject).lineStyle = style;
            } else if (obj.type === 'RECTANGLE') {
              (obj as RectObject).lineStyle = style;
            } else if (obj.type === 'CIRCLE') {
              (obj as CircleObject).lineStyle = style;
            } else if (obj.type === 'POLYGON') {
              (obj as PolygonObject).lineStyle = style;
            } else if (obj.type === 'TRIANGLE') {
              (obj as TriangleObject).lineStyle = style;
            } else if (obj.type === 'STAR') {
              (obj as StarObject).lineStyle = style;
            } else if (obj.type === 'HEART') {
              (obj as HeartObject).lineStyle = style;
            } else if (obj.type === 'DIAMOND') {
              (obj as DiamondObject).lineStyle = style;
            }
          });
          this.renderCanvas();
          this.dispatchChangeEvent();
        }
      });
    });

    // 滤镜控制器
    const filterBrightness = this.shadow.querySelector('.filter-brightness') as HTMLInputElement;
    const filterContrast = this.shadow.querySelector('.filter-contrast') as HTMLInputElement;
    const filterResetBtn = this.shadow.querySelector('.filter-reset-btn');

    const updateFilterValue = (slider: HTMLInputElement) => {
      const valueSpan = slider.parentElement?.querySelector('.filter-value');
      if (valueSpan) {
        valueSpan.textContent = `${slider.value}%`;
      }
    };

    // 实时更新滤镜（不保存历史）
    const applyFiltersToSelectedImage = (saveToHistory: boolean = false) => {
      const selectedObjs = this.getSelectedObjects();
      const imageObj = selectedObjs.find(o => o.type === 'IMAGE') as ImageObject | undefined;
      if (imageObj) {
        if (saveToHistory) {
          this.saveHistory();
        }
        if (!imageObj.filters) {
          imageObj.filters = {};
        }
        imageObj.filters.brightness = parseInt(filterBrightness?.value || '100');
        imageObj.filters.contrast = parseInt(filterContrast?.value || '100');
        this.renderCanvas();
        if (saveToHistory) {
          this.dispatchChangeEvent();
        }
      }
    };

    if (filterBrightness) {
      // 实时预览（拖动时）
      filterBrightness.addEventListener('input', () => {
        updateFilterValue(filterBrightness);
        applyFiltersToSelectedImage(false);
      });
      // 释放时保存历史
      filterBrightness.addEventListener('change', () => {
        applyFiltersToSelectedImage(true);
      });
    }

    if (filterContrast) {
      // 实时预览（拖动时）
      filterContrast.addEventListener('input', () => {
        updateFilterValue(filterContrast);
        applyFiltersToSelectedImage(false);
      });
      // 释放时保存历史
      filterContrast.addEventListener('change', () => {
        applyFiltersToSelectedImage(true);
      });
    }

    if (filterResetBtn) {
      filterResetBtn.addEventListener('click', () => {
        const selectedObjs = this.getSelectedObjects();
        const imageObj = selectedObjs.find(o => o.type === 'IMAGE') as ImageObject | undefined;
        if (imageObj) {
          this.saveHistory();
          // 重置滤镜值为默认值
          imageObj.filters = { brightness: 100, contrast: 100 };
          // 同步更新滑块值和显示
          if (filterBrightness) {
            filterBrightness.value = '100';
            updateFilterValue(filterBrightness);
          }
          if (filterContrast) {
            filterContrast.value = '100';
            updateFilterValue(filterContrast);
          }
          this.renderCanvas();
          this.dispatchChangeEvent();
        }
      });
    }

    // 颜色选择器
    const colorPicker = this.shadow.querySelector('.color-picker') as HTMLInputElement;
    if (colorPicker) {
      const handleColorChange = (e: Event) => {
        const newColor = (e.target as HTMLInputElement).value;
        this.color = newColor;

        // 如果有选中的对象，更新其颜色
        if (this.selectedId) {
          const obj = this.objects.find(o => o.id === this.selectedId);
          if (obj) {
            this.saveHistory();
            this.updateObjectColor(obj, newColor);
            this.renderCanvas();
            this.dispatchChangeEvent();
          }
        } else if (this.selectedIds.size > 0) {
          // 如果有多选对象，更新所有选中对象的颜色
          this.saveHistory();
          this.selectedIds.forEach(id => {
            const obj = this.objects.find(o => o.id === id);
            if (obj) {
              this.updateObjectColor(obj, newColor);
            }
          });
          this.renderCanvas();
          this.dispatchChangeEvent();
        }
      };
      colorPicker.addEventListener('input', handleColorChange);
      colorPicker.addEventListener('change', handleColorChange);
    }

    // 图片上传（支持下拉菜单中的多个）
    this.shadow.querySelectorAll('.image-input').forEach(input => {
      input.addEventListener('change', (e) => this.handleImageUpload(e));
    });

    // 缩放按钮
    const zoomInBtn = this.shadow.querySelector('.zoom-in-btn');
    const zoomOutBtn = this.shadow.querySelector('.zoom-out-btn');
    const zoomText = this.shadow.querySelector('.zoom-text');
    if (zoomInBtn) zoomInBtn.addEventListener('click', () => this.zoomIn());
    if (zoomOutBtn) zoomOutBtn.addEventListener('click', () => this.zoomOut());
    if (zoomText) zoomText.addEventListener('click', () => this.resetZoom());

    // 文件操作
    const saveJsonBtn = this.shadow.querySelector('.save-json-btn');
    const loadJsonInput = this.shadow.querySelector('.load-json-input');
    const exportPngBtn = this.shadow.querySelector('.export-png-btn');
    if (saveJsonBtn) saveJsonBtn.addEventListener('click', () => this.saveJson());
    if (loadJsonInput) loadJsonInput.addEventListener('change', (e) => this.loadJson(e));
    if (exportPngBtn) exportPngBtn.addEventListener('click', () => this.exportPng());

    // 清空画布确认弹窗
    const clearCanvasBtn = this.shadow.querySelector('.clear-canvas-btn');
    const clearConfirmPopup = this.shadow.querySelector('.clear-confirm-popup') as HTMLElement;
    const clearConfirmYes = this.shadow.querySelector('.clear-confirm-yes');
    const clearConfirmNo = this.shadow.querySelector('.clear-confirm-no');

    if (clearCanvasBtn && clearConfirmPopup) {
      clearCanvasBtn.addEventListener('click', () => {
        clearConfirmPopup.classList.toggle('show');
      });
    }
    if (clearConfirmYes && clearConfirmPopup) {
      clearConfirmYes.addEventListener('click', () => {
        this.clearCanvas();
        clearConfirmPopup.classList.remove('show');
      });
    }
    if (clearConfirmNo && clearConfirmPopup) {
      clearConfirmNo.addEventListener('click', () => {
        clearConfirmPopup.classList.remove('show');
      });
    }

    // 文本输入
    if (this.textInput) {
      this.textInput.addEventListener('keydown', (e) => {
        if (e.key === 'Enter') {
          e.preventDefault();
          this.submitText();
        } else if (e.key === 'Escape') {
          this.hideTextInput();
        }
      });
      this.textInput.addEventListener('blur', () => {
        if (this.isTextInputVisible) {
          this.submitText();
        }
      });
    }
  }

  // 创建工具按钮 HTML
  private createToolButton(tool: ToolType, iconClass: string, title: string): string {
    const icons: Record<string, string> = {
      'select-icon': '<path d="M3 3l7.07 16.97 2.51-7.39 7.39-2.51L3 3z"/><path d="M13 13l6 6"/>',
      'pencil-icon': '<path d="M17 3a2.85 2.85 0 114 4L7.5 20.5 2 22l1.5-5.5L17 3z"/>',
      'rect-icon': '<rect x="3" y="3" width="18" height="18" rx="2"/>',
      'circle-icon': '<circle cx="12" cy="12" r="10"/>',
      'text-icon': '<path d="M4 7V4h16v3M9 20h6M12 4v16"/>',
      'line-icon': '<line x1="5" y1="19" x2="19" y2="5"/>',
      'arrow-icon': '<line x1="5" y1="19" x2="19" y2="5"/><polyline points="19 12 19 5 12 5"/>',
      'polygon-icon': '<polygon points="12 2 22 8.5 22 15.5 12 22 2 15.5 2 8.5 12 2"/>',
      'triangle-icon': '<polygon points="12 3 22 21 2 21"/>',
      'star-icon': '<polygon points="12 2 15.09 8.26 22 9.27 17 14.14 18.18 21.02 12 17.77 5.82 21.02 7 14.14 2 9.27 8.91 8.26 12 2"/>',
      'heart-icon': '<path d="M20.84 4.61a5.5 5.5 0 0 0-7.78 0L12 5.67l-1.06-1.06a5.5 5.5 0 0 0-7.78 7.78l1.06 1.06L12 21.23l7.78-7.78 1.06-1.06a5.5 5.5 0 0 0 0-7.78z"/>',
      'diamond-icon': '<polygon points="12 2 22 12 12 22 2 12"/>',
      'bezier-icon': '<path d="M3 17c3-6 9-6 12 0s9 6 12 0"/><circle cx="3" cy="17" r="2"/><circle cx="21" cy="17" r="2"/>',
    };
    const isActive = this.tool === tool;
    return `
      <button class="tool-btn ${isActive ? 'active' : ''}" data-tool="${tool}" title="${title}">
        <svg class="icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
          ${icons[iconClass]}
        </svg>
      </button>
    `;
  }

  // 应用线条样式
  private applyLineStyle(ctx: CanvasRenderingContext2D, lineStyle?: LineStyle): void {
    switch (lineStyle) {
      case 'dashed':
        ctx.setLineDash([10, 5]);
        break;
      case 'dotted':
        ctx.setLineDash([3, 3]);
        break;
      case 'solid':
      default:
        ctx.setLineDash([]);
        break;
    }
  }

  // 将 hex 颜色转换为 rgba
  private hexToRgba(hex: string, alpha: number): string {
    const result = /^#?([a-f\d]{2})([a-f\d]{2})([a-f\d]{2})$/i.exec(hex);
    if (result) {
      const r = parseInt(result[1], 16);
      const g = parseInt(result[2], 16);
      const b = parseInt(result[3], 16);
      return `rgba(${r}, ${g}, ${b}, ${alpha})`;
    }
    return `rgba(84, 80, 220, ${alpha})`; // 默认颜色
  }

  // ========== 形状选择器面板 API ==========

  /**
   * 注册自定义形状到形状面板
   * @param shapes 形状配置数组
   * @param panelConfig 可选的面板配置
   */
  public registerShapes(shapes: ShapeConfig[], panelConfig?: ShapePanelConfig): void {
    // 合并形状配置
    shapes.forEach(shape => {
      const existingIndex = this.registeredShapes.findIndex(s => s.id === shape.id);
      if (existingIndex >= 0) {
        this.registeredShapes[existingIndex] = shape;
      } else {
        this.registeredShapes.push(shape);
      }
    });

    // 更新面板配置
    if (panelConfig) {
      this.shapePanelConfig = { ...this.shapePanelConfig, ...panelConfig };
    }

    // 如果面板已显示，更新内容
    if (this.shapePanelVisible) {
      this.renderShapePanelContent();
    }

    // 触发事件
    this.dispatchEvent(new CustomEvent('shapes-registered', {
      detail: { shapes: this.registeredShapes }
    }));
  }

  /**
   * 获取已注册的形状列表
   */
  public getRegisteredShapes(): ShapeConfig[] {
    return [...this.registeredShapes];
  }

  /**
   * 清空已注册的形状
   */
  public clearRegisteredShapes(): void {
    this.registeredShapes = [];
    if (this.shapePanelVisible) {
      this.renderShapePanelContent();
    }
  }

  /**
   * 移除指定形状
   */
  public removeShape(shapeId: string): void {
    this.registeredShapes = this.registeredShapes.filter(s => s.id !== shapeId);
    if (this.shapePanelVisible) {
      this.renderShapePanelContent();
    }
  }

  /**
   * 显示形状面板
   */
  public showShapePanel(): void {
    if (!this.shapePanelElement) return;
    this.shapePanelVisible = true;
    this.shapePanelElement.style.display = 'block';
    this.renderShapePanelContent();
  }

  /**
   * 隐藏形状面板
   */
  public hideShapePanel(): void {
    if (!this.shapePanelElement) return;
    this.shapePanelVisible = false;
    this.shapePanelElement.style.display = 'none';
  }

  /**
   * 切换形状面板显示状态
   */
  public toggleShapePanel(): void {
    if (this.shapePanelVisible) {
      this.hideShapePanel();
    } else {
      this.showShapePanel();
    }
  }

  // 绑定形状面板事件
  private bindShapePanelEvents(): void {
    // 形状库工具组 - 悬停时渲染内容
    const shapeLibraryGroup = this.shadow.querySelector('.shape-library-group');
    if (shapeLibraryGroup) {
      // 鼠标进入时渲染形状列表
      shapeLibraryGroup.addEventListener('mouseenter', () => {
        this.renderShapeLibraryContent();
      });
    }
  }

  // 渲染形状库内容（工具栏下拉面板）
  private renderShapeLibraryContent(): void {
    const gridContainer = this.shadow.querySelector('.shape-library-grid');
    const emptyContainer = this.shadow.querySelector('.shape-library-empty');
    const shapeLibraryGroup = this.shadow.querySelector('.shape-library-group');
    const searchInput = this.shadow.querySelector('.shape-library-search') as HTMLInputElement;

    if (!gridContainer) return;

    const shapes = this.registeredShapes;

    // 如果没有注册形状，隐藏整个形状库按钮
    if (shapeLibraryGroup) {
      (shapeLibraryGroup as HTMLElement).style.display = shapes.length === 0 ? 'none' : '';
    }

    if (shapes.length === 0) {
      gridContainer.innerHTML = '';
      if (emptyContainer) {
        (emptyContainer as HTMLElement).style.display = 'block';
      }
      return;
    }

    if (emptyContainer) {
      (emptyContainer as HTMLElement).style.display = 'none';
    }

    // 渲染形状列表的函数
    const renderShapes = (filterText: string = '') => {
      const filteredShapes = filterText
        ? shapes.filter(s => s.name.toLowerCase().includes(filterText.toLowerCase()) ||
                            (s.category && s.category.toLowerCase().includes(filterText.toLowerCase())))
        : shapes;

      if (filteredShapes.length === 0) {
        gridContainer.innerHTML = `<div class="shape-library-no-results">${this.t('noResults') || '无匹配结果'}</div>`;
        return;
      }

      gridContainer.innerHTML = filteredShapes.map(shape => `
        <div class="shape-library-item" data-shape-id="${shape.id}" title="${shape.name}${shape.text ? ' - ' + shape.text : ''}">
          <div class="shape-library-item-preview">
            ${this.getShapePreviewSvg(shape)}
          </div>
          <span class="shape-library-item-name">${shape.name}</span>
        </div>
      `).join('');

      // 绑定形状点击事件
      gridContainer.querySelectorAll('.shape-library-item').forEach(item => {
        item.addEventListener('click', (e) => {
          const shapeId = (e.currentTarget as HTMLElement).dataset.shapeId;
          const shape = this.registeredShapes.find(s => s.id === shapeId);
          if (shape) {
            this.addShapeToCanvas(shape);
          }
        });
      });
    };

    // 初始渲染
    renderShapes();

    // 绑定搜索事件
    if (searchInput) {
      searchInput.addEventListener('input', (e) => {
        renderShapes((e.target as HTMLInputElement).value);
      });
    }
  }

  // 渲染形状面板内容（保留旧方法兼容）
  private renderShapePanelContent(): void {
    // 直接调用新方法
    this.renderShapeLibraryContent();
  }

  // 获取形状预览SVG
  private getShapePreviewSvg(shape: ShapeConfig): string {
    // 如果有自定义图标，使用自定义图标
    if (shape.icon) {
      return shape.icon;
    }

    const fillColor = shape.fillColor || '#5450dc';
    const strokeColor = shape.strokeColor || fillColor;
    const strokeWidth = shape.strokeWidth || 2;
    const fill = shape.fillMode === 'stroke' ? 'none' : fillColor;
    const stroke = shape.fillMode === 'fill' ? 'none' : strokeColor;

    // 根据形状类型生成预览SVG（不显示文字，文字在下方名称显示）
    switch (shape.type) {
      case 'rectangle':
        return `<svg viewBox="0 0 36 36" fill="${fill}" stroke="${stroke}" stroke-width="${strokeWidth}">
          <rect x="4" y="8" width="28" height="20" rx="${shape.cornerRadius || 2}"/>
        </svg>`;
      case 'circle':
        return `<svg viewBox="0 0 36 36" fill="${fill}" stroke="${stroke}" stroke-width="${strokeWidth}">
          <circle cx="18" cy="18" r="14"/>
        </svg>`;
      case 'ellipse':
        return `<svg viewBox="0 0 36 36" fill="${fill}" stroke="${stroke}" stroke-width="${strokeWidth}">
          <ellipse cx="18" cy="18" rx="16" ry="10"/>
        </svg>`;
      case 'triangle':
        return `<svg viewBox="0 0 36 36" fill="${fill}" stroke="${stroke}" stroke-width="${strokeWidth}">
          <polygon points="18 4 32 32 4 32"/>
        </svg>`;
      case 'star':
        return `<svg viewBox="0 0 36 36" fill="${fill}" stroke="${stroke}" stroke-width="${strokeWidth}">
          <polygon points="18 2 22.5 13 34 13 25 21 28.5 32 18 25 7.5 32 11 21 2 13 13.5 13"/>
        </svg>`;
      case 'heart':
        return `<svg viewBox="0 0 36 36" fill="${fill}" stroke="${stroke}" stroke-width="${strokeWidth}">
          <path d="M18 32 C8 22 2 16 2 10 C2 5 6 2 11 2 C14 2 17 4 18 6 C19 4 22 2 25 2 C30 2 34 5 34 10 C34 16 28 22 18 32Z"/>
        </svg>`;
      case 'diamond':
        return `<svg viewBox="0 0 36 36" fill="${fill}" stroke="${stroke}" stroke-width="${strokeWidth}">
          <polygon points="18 2 34 18 18 34 2 18"/>
        </svg>`;
      case 'hexagon':
        return `<svg viewBox="0 0 36 36" fill="${fill}" stroke="${stroke}" stroke-width="${strokeWidth}">
          <polygon points="9 4 27 4 34 18 27 32 9 32 2 18"/>
        </svg>`;
      case 'polygon':
        const sides = shape.sides || 6;
        const points = this.generatePolygonPoints(18, 18, 14, sides);
        return `<svg viewBox="0 0 36 36" fill="${fill}" stroke="${stroke}" stroke-width="${strokeWidth}">
          <polygon points="${points}"/>
        </svg>`;
      case 'roundedRect':
        return `<svg viewBox="0 0 36 36" fill="${fill}" stroke="${stroke}" stroke-width="${strokeWidth}">
          <rect x="4" y="8" width="28" height="20" rx="${shape.cornerRadius || 8}"/>
        </svg>`;
      case 'parallelogram':
        return `<svg viewBox="0 0 36 36" fill="${fill}" stroke="${stroke}" stroke-width="${strokeWidth}">
          <polygon points="8 4 34 4 28 32 2 32"/>
        </svg>`;
      case 'trapezoid':
        return `<svg viewBox="0 0 36 36" fill="${fill}" stroke="${stroke}" stroke-width="${strokeWidth}">
          <polygon points="8 8 28 8 34 28 2 28"/>
        </svg>`;
      case 'arrow':
        return `<svg viewBox="0 0 36 36" fill="${fill}" stroke="${stroke}" stroke-width="${strokeWidth}">
          <polygon points="2 14 22 14 22 6 34 18 22 30 22 22 2 22"/>
        </svg>`;
      case 'callout':
        return `<svg viewBox="0 0 36 36" fill="${fill}" stroke="${stroke}" stroke-width="${strokeWidth}">
          <path d="M4 4 H32 V24 H16 L8 32 L10 24 H4 Z"/>
        </svg>`;
      case 'cloud':
        return `<svg viewBox="0 0 36 36" fill="${fill}" stroke="${stroke}" stroke-width="${strokeWidth}">
          <path d="M8 28 C4 28 2 24 4 20 C2 16 6 12 10 14 C12 8 20 8 22 12 C26 10 32 14 30 20 C34 22 32 28 28 28 Z"/>
        </svg>`;
      case 'cross':
        return `<svg viewBox="0 0 36 36" fill="${fill}" stroke="${stroke}" stroke-width="${strokeWidth}">
          <polygon points="14 2 22 2 22 14 34 14 34 22 22 22 22 34 14 34 14 22 2 22 2 14 14 14"/>
        </svg>`;
      default:
        return `<svg viewBox="0 0 36 36" fill="${fill}" stroke="${stroke}" stroke-width="${strokeWidth}">
          <rect x="4" y="4" width="28" height="28" rx="4"/>
        </svg>`;
    }
  }

  // 生成多边形顶点
  private generatePolygonPoints(cx: number, cy: number, radius: number, sides: number): string {
    const points: string[] = [];
    for (let i = 0; i < sides; i++) {
      const angle = (i * 2 * Math.PI / sides) - Math.PI / 2;
      const x = cx + radius * Math.cos(angle);
      const y = cy + radius * Math.sin(angle);
      points.push(`${x.toFixed(1)},${y.toFixed(1)}`);
    }
    return points.join(' ');
  }

  // 添加形状到画布
  private addShapeToCanvas(shape: ShapeConfig): void {
    // 计算画布中心位置
    const centerX = (this.canvas.width / 2 - this.panOffset.x) / this.scale;
    const centerY = (this.canvas.height / 2 - this.panOffset.y) / this.scale;

    const width = shape.width || 100;
    const height = shape.height || 100;

    // 保存当前状态到历史
    this.saveHistory();
    // 立即隐藏空画布提示
    this.hideEmptyHint();

    let newObject: CanvasObject;

    // 根据填充模式决定使用哪个颜色
    // fill 模式：使用填充颜色
    // stroke 模式：使用边框颜色
    // both 模式：使用填充颜色（边框颜色由 lineWidth > 0 时自动使用 color）
    const shapeColor = shape.fillMode === 'stroke'
      ? (shape.strokeColor || shape.fillColor || '#000000')
      : (shape.fillColor || shape.strokeColor || '#000000');

    // 根据形状类型创建对应的画布对象
    switch (shape.type) {
      case 'rectangle':
      case 'roundedRect':
      case 'parallelogram':
      case 'trapezoid':
        newObject = {
          id: this.generateId(),
          type: 'RECTANGLE',
          x: centerX - width / 2,
          y: centerY - height / 2,
          width,
          height,
          color: shapeColor,
          lineWidth: shape.strokeWidth || 2,
          fillMode: shape.fillMode || 'both',
          lineStyle: shape.strokeStyle || 'solid',
          opacity: shape.opacity
        } as RectObject;
        break;

      case 'circle':
      case 'ellipse':
        newObject = {
          id: this.generateId(),
          type: 'CIRCLE',
          x: centerX,
          y: centerY,
          radius: Math.min(width, height) / 2,
          color: shapeColor,
          lineWidth: shape.strokeWidth || 2,
          fillMode: shape.fillMode || 'both',
          lineStyle: shape.strokeStyle || 'solid',
          opacity: shape.opacity
        } as CircleObject;
        break;

      case 'triangle':
        newObject = {
          id: this.generateId(),
          type: 'TRIANGLE',
          x: centerX,
          y: centerY,
          radius: Math.min(width, height) / 2,
          color: shapeColor,
          lineWidth: shape.strokeWidth || 2,
          fillMode: shape.fillMode || 'both',
          lineStyle: shape.strokeStyle || 'solid',
          opacity: shape.opacity
        } as TriangleObject;
        break;

      case 'star':
        newObject = {
          id: this.generateId(),
          type: 'STAR',
          x: centerX,
          y: centerY,
          points: shape.points || 5,
          outerRadius: Math.min(width, height) / 2,
          innerRadius: shape.innerRadius || Math.min(width, height) / 4,
          color: shapeColor,
          lineWidth: shape.strokeWidth || 2,
          fillMode: shape.fillMode || 'both',
          lineStyle: shape.strokeStyle || 'solid',
          opacity: shape.opacity
        } as StarObject;
        break;

      case 'heart':
        newObject = {
          id: this.generateId(),
          type: 'HEART',
          x: centerX,
          y: centerY,
          size: Math.min(width, height) / 2,
          color: shapeColor,
          lineWidth: shape.strokeWidth || 2,
          fillMode: shape.fillMode || 'both',
          lineStyle: shape.strokeStyle || 'solid',
          opacity: shape.opacity
        } as HeartObject;
        break;

      case 'diamond':
        newObject = {
          id: this.generateId(),
          type: 'DIAMOND',
          x: centerX,
          y: centerY,
          width,
          height,
          color: shapeColor,
          lineWidth: shape.strokeWidth || 2,
          fillMode: shape.fillMode || 'both',
          lineStyle: shape.strokeStyle || 'solid',
          opacity: shape.opacity
        } as DiamondObject;
        break;

      case 'polygon':
      case 'hexagon':
      case 'octagon':
        newObject = {
          id: this.generateId(),
          type: 'POLYGON',
          x: centerX,
          y: centerY,
          sides: shape.sides || (shape.type === 'hexagon' ? 6 : shape.type === 'octagon' ? 8 : 6),
          radius: Math.min(width, height) / 2,
          color: shapeColor,
          lineWidth: shape.strokeWidth || 2,
          fillMode: shape.fillMode || 'both',
          lineStyle: shape.strokeStyle || 'solid',
          opacity: shape.opacity
        } as PolygonObject;
        break;

      default:
        // 默认创建矩形
        newObject = {
          id: this.generateId(),
          type: 'RECTANGLE',
          x: centerX - width / 2,
          y: centerY - height / 2,
          width,
          height,
          color: shapeColor,
          lineWidth: shape.strokeWidth || 2,
          fillMode: shape.fillMode || 'both',
          lineStyle: shape.strokeStyle || 'solid',
          opacity: shape.opacity
        } as RectObject;
    }

    // 添加到对象列表
    this.objects.push(newObject);

    // 如果形状有文字，创建富文本对象并与形状组合
    if (shape.text) {
      const richTextObject: RichTextObject = {
        id: this.generateId(),
        type: 'RICH_TEXT',
        x: 0,  // 相对于 GROUP 中心的坐标
        y: 0,
        fontSize: shape.fontSize || 16,
        fontFamily: shape.fontFamily || 'sans-serif',
        color: shape.textColor || '#000000',
        lineWidth: 1,
        textAlign: 'center',
        segments: [{
          text: shape.text,
          color: shape.textColor || '#000000',
          fontSize: shape.fontSize || 16,
          fontFamily: shape.fontFamily || 'sans-serif',
          bold: shape.fontWeight === 'bold',
          italic: shape.fontStyle === 'italic'
        }]
      };

      // 计算组合的边界
      const groupWidth = width;
      const groupHeight = height;

      // 将形状对象的坐标转换为相对于 GROUP 中心的坐标
      const shapeRelativeX = newObject.x - centerX;
      const shapeRelativeY = newObject.y - centerY;
      const relativeShapeObject = { ...newObject, x: shapeRelativeX, y: shapeRelativeY };

      // 创建组合对象，包含形状和富文本（子对象使用相对坐标）
      const groupObject: GroupObject = {
        id: this.generateId(),
        type: 'GROUP',
        x: centerX,
        y: centerY,
        children: [relativeShapeObject as CanvasObject, richTextObject],
        width: groupWidth,
        height: groupHeight,
        color: '#000000',
        lineWidth: 1
      };

      // 从对象列表中移除单独的形状（它现在在组合中）
      this.objects = this.objects.filter(obj => obj.id !== newObject.id);
      this.objects.push(groupObject);

      // 选中组合对象
      this.selectedId = groupObject.id;
      this.selectedIds.clear();
      this.selectedIds.add(groupObject.id);
    } else {
      // 选中新创建的形状对象
      this.selectedId = newObject.id;
      this.selectedIds.clear();
      this.selectedIds.add(newObject.id);
    }

    // 切换到选择工具
    this.setTool('SELECT');

    // 重新渲染
    this.renderCanvas();
    this.dispatchChangeEvent();

    // 触发形状添加事件
    this.dispatchEvent(new CustomEvent('shape-added', {
      detail: { shape, object: newObject }
    }));
  }

  // 获取样式
  private getStyles(): string {
    const themeColor = this.config.themeColor || DEFAULT_THEME_COLOR;
    // 计算悬停颜色（主题色加透明度）
    const hoverBgColor = this.hexToRgba(themeColor, 0.1);
    const shadowColor = this.hexToRgba(themeColor, 0.3);

    return `
      :host {
        display: block;
        width: 100%;
        height: 100%;
        font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif;
        --theme-color: ${themeColor};
        --theme-hover-bg: ${hoverBgColor};
        --theme-shadow: ${shadowColor};
      }

      * {
        box-sizing: border-box;
        margin: 0;
        padding: 0;
      }

      .hidden {
        display: none !important;
      }

      .editor-container {
        display: flex;
        width: 100%;
        height: 100%;
        background: #f1f5f9;
        position: relative;
      }

      /* Overlay mode: transparent drawing surface over media (host has the "overlay" attribute).
         The top bar is hidden and the toolbar floats so the canvas fills the host exactly. */
      :host([overlay]) .editor-container,
      :host([overlay]) .canvas-container {
        background: transparent;
      }
      :host([overlay]) .top-bar {
        display: none;
      }
      :host([overlay]) .toolbar {
        position: absolute;
        left: 8px;
        top: 50%;
        transform: translateY(-50%);
        max-height: calc(100% - 16px);
        overflow-y: auto;
        z-index: 20;
        border: 1px solid #e2e8f0;
        border-radius: 12px;
        box-shadow: 0 4px 12px rgba(0, 0, 0, 0.15);
      }

      /* 工具栏 */
      .toolbar {
        width: 64px;
        background: #ffffff;
        border-right: 1px solid #e2e8f0;
        display: flex;
        flex-direction: column;
        align-items: center;
        padding: 12px 8px;
        gap: 4px;
      }

      .tool-btn {
        width: 44px;
        height: 44px;
        border: none;
        background: transparent;
        border-radius: 12px;
        cursor: pointer;
        display: flex;
        align-items: center;
        justify-content: center;
        color: #64748b;
        transition: all 0.2s;
      }

      .tool-btn:hover {
        background: var(--theme-hover-bg);
        color: var(--theme-color);
      }

      .tool-btn.active {
        background: var(--theme-color);
        color: #ffffff;
        box-shadow: 0 4px 12px var(--theme-shadow);
        transform: scale(1.05);
      }

      .tool-btn:disabled {
        color: #cbd5e1;
        cursor: not-allowed;
      }

      .tool-btn:disabled:hover {
        background: transparent;
        color: #cbd5e1;
      }

      .icon {
        width: 20px;
        height: 20px;
      }

      /* 工具组样式 */
      .tool-group {
        position: relative;
      }

      .tool-group-btn {
        position: relative;
      }

      .dropdown-indicator {
        position: absolute;
        bottom: 4px;
        right: 4px;
        font-size: 8px;
        opacity: 0.6;
      }

      .tool-dropdown {
        display: none;
        position: absolute;
        left: 100%;
        top: 0;
        background: #ffffff;
        border-radius: 12px;
        box-shadow: 0 8px 32px rgba(0, 0, 0, 0.12);
        padding: 8px;
        min-width: 160px;
        z-index: 100;
      }

      /* 添加透明连接区域，确保鼠标移动时不会丢失hover */
      .tool-dropdown::before {
        content: '';
        position: absolute;
        left: -12px;
        top: 0;
        width: 12px;
        height: 100%;
      }

      .tool-group:hover .tool-dropdown,
      .tool-group.active .tool-dropdown {
        display: flex;
        flex-direction: column;
        gap: 4px;
      }

      .tool-dropdown .tool-btn {
        width: 100%;
        height: 40px;
        justify-content: flex-start;
        padding: 0 12px;
        gap: 8px;
      }

      .tool-dropdown .tool-btn.active {
        transform: none;
      }

      .dropdown-item {
        cursor: pointer;
      }

      .dropdown-label {
        font-size: 13px;
        white-space: nowrap;
      }

      .divider {
        width: 32px;
        height: 1px;
        background: #e2e8f0;
        margin: 8px 0;
      }

      .spacer {
        flex: 1;
      }

      /* 填充模式选择器 */
      .fill-mode-group {
        display: flex;
        gap: 2px;
        padding: 4px;
        background: #f1f5f9;
        border-radius: 6px;
        flex-shrink: 0;
      }

      .fill-mode-btn {
        width: 28px;
        height: 28px;
        padding: 4px;
        border-radius: 4px;
      }

      .fill-mode-btn.active {
        background: var(--theme-color, ${DEFAULT_THEME_COLOR});
        color: white;
      }

      /* 线条样式选择器 */
      .line-style-group {
        display: flex;
        gap: 2px;
        padding: 4px;
        background: #f1f5f9;
        border-radius: 6px;
        flex-shrink: 0;
      }

      .line-style-btn {
        width: 28px;
        height: 28px;
        padding: 4px;
        border-radius: 4px;
      }

      .line-style-btn.active {
        background: var(--theme-color, ${DEFAULT_THEME_COLOR});
        color: white;
      }

      /* 滤镜控制面板 */
      .filter-controls {
        display: flex;
        align-items: center;
        gap: 8px;
        padding: 4px 12px;
        background: #f8fafc;
        border-radius: 8px;
        border: 1px solid #e2e8f0;
        flex-wrap: wrap;
        flex-shrink: 0;
      }

      .filter-label {
        font-size: 12px;
        font-weight: 600;
        color: #64748b;
        flex-shrink: 0;
      }

      .filter-item {
        display: flex;
        align-items: center;
        gap: 6px;
        flex-shrink: 0;
      }

      .filter-item label {
        font-size: 11px;
        color: #64748b;
        min-width: 32px;
      }

      .filter-slider {
        width: 80px;
        height: 20px;
        -webkit-appearance: none;
        appearance: none;
        background: transparent;
        cursor: pointer;
        margin: 0;
        padding: 0;
        position: relative;
      }

      .filter-slider::-webkit-slider-runnable-track {
        width: 100%;
        height: 6px;
        background: #e2e8f0;
        border-radius: 3px;
        cursor: pointer;
      }

      .filter-slider::-webkit-slider-thumb {
        -webkit-appearance: none;
        appearance: none;
        width: 16px;
        height: 16px;
        background: var(--theme-color, ${DEFAULT_THEME_COLOR});
        border-radius: 50%;
        cursor: pointer;
        margin-top: -5px;
        box-shadow: 0 1px 3px rgba(0,0,0,0.2);
      }

      .filter-slider::-webkit-slider-thumb:hover {
        transform: scale(1.1);
      }

      .filter-slider::-moz-range-track {
        width: 100%;
        height: 6px;
        background: #e2e8f0;
        border-radius: 3px;
        cursor: pointer;
      }

      .filter-slider::-moz-range-thumb {
        width: 16px;
        height: 16px;
        background: var(--theme-color, ${DEFAULT_THEME_COLOR});
        border-radius: 50%;
        cursor: pointer;
        border: none;
        box-shadow: 0 1px 3px rgba(0,0,0,0.2);
      }

      .filter-slider::-moz-range-thumb:hover {
        transform: scale(1.1);
      }

      /* 增加滑块点击区域 */
      .filter-slider:focus {
        outline: none;
      }

      .filter-slider:focus::-webkit-slider-thumb {
        box-shadow: 0 0 0 3px rgba(139, 92, 246, 0.2);
      }

      .filter-slider:focus::-moz-range-thumb {
        box-shadow: 0 0 0 3px rgba(139, 92, 246, 0.2);
      }

      .filter-value {
        font-size: 11px;
        color: #64748b;
        min-width: 35px;
        text-align: right;
      }

      .filter-reset-btn {
        background: none;
        border: none;
        color: #64748b;
        cursor: pointer;
        padding: 4px;
        display: flex;
        align-items: center;
        justify-content: center;
        border-radius: 4px;
      }

      .filter-reset-btn:hover {
        background: #e2e8f0;
        color: #334155;
      }

      .filter-reset-btn .icon {
        width: 16px;
        height: 16px;
      }

      .color-picker {
        width: 32px;
        height: 32px;
        border: 2px solid #e2e8f0;
        border-radius: 50%;
        cursor: pointer;
        padding: 0;
        overflow: hidden;
        -webkit-appearance: none;
      }

      .color-picker::-webkit-color-swatch-wrapper {
        padding: 0;
      }

      .color-picker::-webkit-color-swatch {
        border: none;
        border-radius: 50%;
      }

      /* 主区域 */
      .main-area {
        flex: 1;
        display: flex;
        flex-direction: column;
        overflow: hidden;
      }

      /* 顶部栏 */
      .top-bar {
        min-height: 56px;
        background: #ffffff;
        border-bottom: 1px solid #e2e8f0;
        display: flex;
        align-items: center;
        justify-content: space-between;
        padding: 8px 16px;
        box-shadow: 0 1px 3px rgba(0, 0, 0, 0.05);
        flex-wrap: wrap;
        gap: 8px;
      }

      .top-bar-left {
        display: flex;
        align-items: center;
        gap: 16px;
        flex-shrink: 0;
      }

      .top-bar-right {
        display: flex;
        align-items: center;
        gap: 12px;
        flex-wrap: wrap;
        flex: 1;
        justify-content: flex-end;
      }

      .title {
        font-size: 16px;
        font-weight: 600;
        color: #334155;
      }

      .selection-info {
        display: none;
        align-items: center;
        gap: 8px;
        background: #eef2ff;
        padding: 4px 12px;
        border-radius: 20px;
        border: 1px solid #c7d2fe;
      }

      .selection-info.visible {
        display: flex;
      }

      .selection-label {
        font-size: 12px;
        font-weight: 600;
        color: var(--theme-color);
        text-transform: uppercase;
      }

      .delete-btn {
        background: none;
        border: none;
        color: #ef4444;
        cursor: pointer;
        padding: 4px;
        display: flex;
        align-items: center;
        justify-content: center;
      }

      .delete-btn:hover {
        color: #dc2626;
      }

      .align-controls {
        display: flex;
        align-items: center;
        background: #f1f5f9;
        border-radius: 8px;
        padding: 4px;
        gap: 2px;
        flex-shrink: 0;
      }

      .align-btn {
        width: 32px;
        height: 32px;
        border: none;
        background: transparent;
        border-radius: 6px;
        cursor: pointer;
        display: flex;
        align-items: center;
        justify-content: center;
        color: #64748b;
        transition: all 0.2s;
      }

      .align-btn:hover {
        background: var(--theme-hover-bg);
        color: var(--theme-color);
      }

      .align-btn:active {
        background: var(--theme-color);
        color: #ffffff;
      }

      .align-btn .icon {
        width: 16px;
        height: 16px;
      }

      .zoom-controls, .file-controls {
        display: flex;
        align-items: center;
        background: #f1f5f9;
        border-radius: 8px;
        padding: 4px;
        flex-shrink: 0;
      }

      .zoom-btn, .file-btn {
        width: 32px;
        height: 32px;
        border: none;
        background: transparent;
        border-radius: 6px;
        cursor: pointer;
        display: flex;
        align-items: center;
        justify-content: center;
        color: #475569;
        transition: all 0.2s;
      }

      .zoom-btn:hover, .file-btn:hover {
        color: var(--theme-color);
      }

      .zoom-text {
        padding: 4px 8px;
        font-size: 12px;
        font-weight: 500;
        color: #475569;
        background: transparent;
        border: none;
        cursor: pointer;
        min-width: 50px;
        text-align: center;
      }

      .zoom-text:hover {
        color: var(--theme-color);
      }

      /* 清空按钮容器 */
      .clear-btn-wrapper {
        position: relative;
      }

      .clear-confirm-popup {
        display: none;
        position: absolute;
        top: 100%;
        right: 0;
        margin-top: 8px;
        background: #ffffff;
        border: 1px solid #e2e8f0;
        border-radius: 8px;
        padding: 12px;
        box-shadow: 0 4px 12px rgba(0, 0, 0, 0.15);
        z-index: 100;
        white-space: nowrap;
      }

      .clear-confirm-popup.show {
        display: block;
      }

      .clear-confirm-text {
        margin: 0 0 10px 0;
        font-size: 13px;
        color: #475569;
      }

      .clear-confirm-actions {
        display: flex;
        gap: 8px;
      }

      .clear-confirm-yes,
      .clear-confirm-no {
        padding: 6px 14px;
        border: none;
        border-radius: 6px;
        font-size: 12px;
        cursor: pointer;
        transition: all 0.15s;
      }

      .clear-confirm-yes {
        background: #ef4444;
        color: #ffffff;
      }

      .clear-confirm-yes:hover {
        background: #dc2626;
      }

      .clear-confirm-no {
        background: #f1f5f9;
        color: #475569;
      }

      .clear-confirm-no:hover {
        background: #e2e8f0;
      }

      /* 画布容器 */
      .canvas-container {
        flex: 1;
        position: relative;
        background: #f1f5f9;
        overflow: hidden;
      }

      .main-canvas {
        position: absolute;
        inset: 0;
        display: block;
        cursor: crosshair;
        touch-action: none;
      }

      /* 文本输入 */
      .text-input-container {
        position: absolute;
        z-index: 20;
        display: flex;
        flex-direction: column;
        align-items: flex-start;
      }

      .text-input-hint {
        background: rgba(0, 0, 0, 0.75);
        color: #fff;
        font-size: 12px;
        padding: 4px 8px;
        border-radius: 4px;
        margin-bottom: 4px;
        white-space: nowrap;
      }

      .text-input {
        padding: 8px 12px;
        border: 2px solid var(--theme-color);
        border-radius: 8px;
        box-shadow: 0 4px 12px rgba(0, 0, 0, 0.15);
        outline: none;
        min-width: 200px;
        font-size: 16px;
      }

      /* 富文本编辑器 */
      .rich-text-editor {
        position: absolute;
        z-index: 30;
        background: #ffffff;
        border-radius: 12px;
        box-shadow: 0 8px 32px rgba(0, 0, 0, 0.2);
        min-width: 320px;
        max-width: 480px;
        overflow: hidden;
      }

      .rich-text-toolbar {
        display: flex;
        align-items: center;
        gap: 4px;
        padding: 8px 12px;
        background: #f8fafc;
        border-bottom: 1px solid #e2e8f0;
      }

      .rich-text-btn {
        width: 32px;
        height: 32px;
        border: none;
        background: transparent;
        border-radius: 6px;
        cursor: pointer;
        display: flex;
        align-items: center;
        justify-content: center;
        color: #475569;
        transition: all 0.15s;
      }

      .rich-text-btn:hover {
        background: #e2e8f0;
        color: var(--theme-color);
      }

      .rich-text-btn.active {
        background: var(--theme-color);
        color: #ffffff;
      }

      .rich-text-divider {
        width: 1px;
        height: 20px;
        background: #e2e8f0;
        margin: 0 4px;
      }

      .rich-text-color {
        width: 32px;
        height: 32px;
        border: none;
        border-radius: 6px;
        cursor: pointer;
        padding: 2px;
      }

      .rich-text-fontsize {
        width: 50px;
        height: 32px;
        border: 1px solid #e2e8f0;
        border-radius: 6px;
        padding: 0 6px;
        font-size: 13px;
        text-align: center;
      }

      .rich-text-segments {
        padding: 12px;
        max-height: 300px;
        overflow-y: auto;
      }

      .rich-text-segment {
        display: flex;
        align-items: center;
        gap: 8px;
        padding: 8px;
        background: #f8fafc;
        border-radius: 8px;
        margin-bottom: 8px;
        border: 2px solid transparent;
        transition: border-color 0.15s;
      }

      .rich-text-segment:last-child {
        margin-bottom: 0;
      }

      .rich-text-segment.selected {
        border-color: var(--theme-color);
      }

      .rich-text-segment-input {
        flex: 1;
        border: none;
        background: transparent;
        font-size: 14px;
        outline: none;
        padding: 4px;
      }

      .rich-text-segment-preview {
        padding: 4px 8px;
        border-radius: 4px;
        background: #ffffff;
        border: 1px solid #e2e8f0;
        font-size: 12px;
        min-width: 60px;
        text-align: center;
      }

      .rich-text-segment-delete {
        width: 24px;
        height: 24px;
        border: none;
        background: transparent;
        border-radius: 4px;
        cursor: pointer;
        color: #94a3b8;
        display: flex;
        align-items: center;
        justify-content: center;
      }

      .rich-text-segment-delete:hover {
        background: #fee2e2;
        color: #ef4444;
      }

      .rich-text-actions {
        display: flex;
        justify-content: flex-end;
        gap: 8px;
        padding: 12px;
        background: #f8fafc;
        border-top: 1px solid #e2e8f0;
      }

      .rich-text-action-btn {
        padding: 8px 16px;
        border: none;
        border-radius: 6px;
        font-size: 14px;
        cursor: pointer;
        transition: all 0.15s;
      }

      .rich-text-action-btn.cancel {
        background: #f1f5f9;
        color: #475569;
      }

      .rich-text-action-btn.cancel:hover {
        background: #e2e8f0;
      }

      .rich-text-action-btn.confirm {
        background: var(--theme-color);
        color: #ffffff;
      }

      .rich-text-action-btn.confirm:hover {
        filter: brightness(0.9);
      }

      /* 动画时间线编辑器 */
      .timeline-editor {
        position: absolute;
        bottom: 20px;
        left: 50%;
        transform: translateX(-50%);
        z-index: 30;
        background: #1e293b;
        border-radius: 12px;
        box-shadow: 0 8px 32px rgba(0, 0, 0, 0.3);
        min-width: 600px;
        max-width: 90%;
        color: #e2e8f0;
        overflow: hidden;
      }

      .timeline-header {
        display: flex;
        align-items: center;
        gap: 16px;
        padding: 12px 16px;
        background: #0f172a;
        border-bottom: 1px solid #334155;
      }

      .timeline-title {
        font-weight: 600;
        font-size: 14px;
        flex: 1;
      }

      .timeline-controls {
        display: flex;
        gap: 8px;
      }

      .timeline-btn {
        width: 32px;
        height: 32px;
        border: none;
        border-radius: 6px;
        background: #334155;
        color: #e2e8f0;
        cursor: pointer;
        font-size: 12px;
        transition: all 0.15s;
      }

      .timeline-btn:hover {
        background: #475569;
      }

      .timeline-btn.play:hover {
        background: #22c55e;
      }

      .timeline-close {
        width: 28px;
        height: 28px;
        border: none;
        border-radius: 6px;
        background: transparent;
        color: #94a3b8;
        cursor: pointer;
        font-size: 18px;
        transition: all 0.15s;
      }

      .timeline-close:hover {
        background: #ef4444;
        color: #ffffff;
      }

      .timeline-content {
        padding: 16px;
        min-height: 120px;
      }

      .timeline-tracks {
        min-height: 60px;
        background: #0f172a;
        border-radius: 6px;
        margin-bottom: 12px;
        padding: 8px;
      }

      .timeline-track {
        display: flex;
        align-items: center;
        gap: 8px;
        padding: 6px 0;
        border-bottom: 1px solid #334155;
      }

      .timeline-track:last-child {
        border-bottom: none;
      }

      .timeline-track-label {
        min-width: 80px;
        font-size: 12px;
        color: #94a3b8;
      }

      .timeline-track-bar {
        flex: 1;
        height: 24px;
        background: #1e293b;
        border-radius: 4px;
        position: relative;
      }

      .timeline-keyframe {
        position: absolute;
        width: 12px;
        height: 12px;
        background: var(--theme-color);
        border-radius: 2px;
        transform: rotate(45deg) translateY(-50%);
        top: 50%;
        cursor: pointer;
        transition: all 0.15s;
      }

      .timeline-keyframe:hover {
        transform: rotate(45deg) translateY(-50%) scale(1.2);
      }

      .timeline-ruler {
        height: 24px;
        background: #0f172a;
        border-radius: 4px;
        position: relative;
      }

      .timeline-playhead {
        position: absolute;
        top: 0;
        left: 0;
        width: 2px;
        height: 100%;
        background: #ef4444;
        transition: left 0.05s linear;
      }

      .timeline-playhead::before {
        content: '';
        position: absolute;
        top: -4px;
        left: -4px;
        width: 10px;
        height: 10px;
        background: #ef4444;
        border-radius: 50%;
      }

      .timeline-footer {
        display: flex;
        align-items: center;
        justify-content: space-between;
        padding: 12px 16px;
        background: #0f172a;
        border-top: 1px solid #334155;
      }

      .timeline-add-keyframe {
        padding: 8px 16px;
        border: none;
        border-radius: 6px;
        background: var(--theme-color);
        color: #ffffff;
        font-size: 13px;
        cursor: pointer;
        transition: all 0.15s;
      }

      .timeline-add-keyframe:hover {
        filter: brightness(0.9);
      }

      .timeline-time {
        font-size: 13px;
        font-family: monospace;
        color: #94a3b8;
      }

      /* 空画布提示 */
      .empty-hint {
        position: absolute;
        inset: 0;
        display: flex;
        flex-direction: column;
        align-items: center;
        justify-content: center;
        pointer-events: none;
        opacity: 0.4;
      }

      .empty-hint h3 {
        font-size: 24px;
        font-weight: 700;
        color: #94a3b8;
        margin-bottom: 8px;
      }

      .empty-hint p {
        color: #94a3b8;
      }

      /* 右键菜单 */
      .context-menu {
        position: absolute;
        z-index: 100;
        background: #ffffff;
        border: 1px solid #e2e8f0;
        border-radius: 8px;
        box-shadow: 0 4px 12px rgba(0, 0, 0, 0.15);
        min-width: 140px;
        padding: 4px 0;
      }

      .context-menu-item {
        padding: 8px 16px;
        font-size: 14px;
        color: #334155;
        cursor: pointer;
        transition: background 0.15s;
      }

      .context-menu-item:hover {
        background: #f1f5f9;
      }

      .context-menu-item-danger {
        color: #ef4444;
      }

      .context-menu-item-danger:hover {
        background: #fef2f2;
      }

      /* 图层面板 */
      .layer-panel {
        position: absolute;
        top: 60px;
        right: 10px;
        width: 240px;
        max-height: 300px;
        background: #ffffff;
        border-radius: 8px;
        box-shadow: 0 4px 20px rgba(0, 0, 0, 0.15);
        z-index: 150;
        overflow: hidden;
      }

      .layer-panel-header {
        display: flex;
        align-items: center;
        justify-content: space-between;
        padding: 10px 14px;
        background: #f8fafc;
        border-bottom: 1px solid #e2e8f0;
        font-weight: 600;
        font-size: 13px;
        color: #334155;
      }

      .layer-panel-close {
        background: none;
        border: none;
        font-size: 18px;
        cursor: pointer;
        color: #64748b;
        padding: 0;
        line-height: 1;
      }

      .layer-panel-close:hover {
        color: #ef4444;
      }

      .layer-panel-list {
        max-height: 250px;
        overflow-y: auto;
      }

      .layer-item {
        display: flex;
        align-items: center;
        gap: 10px;
        padding: 8px 12px;
        min-height: 56px;
        border-bottom: 1px solid #f1f5f9;
        font-size: 12px;
        cursor: pointer;
        transition: background 0.15s;
      }

      .layer-item:hover {
        background: #f8fafc;
      }

      .layer-item.selected {
        background: #ede9fe;
      }

      .layer-item-thumbnail {
        width: 40px;
        height: 40px;
        border-radius: 4px;
        border: 1px solid #e2e8f0;
        background: #f8fafc;
        flex-shrink: 0;
      }

      .layer-item-name {
        flex: 1;
        overflow: hidden;
        text-overflow: ellipsis;
        white-space: nowrap;
      }

      .layer-item-actions {
        display: flex;
        gap: 4px;
      }

      .layer-item-btn {
        background: none;
        border: none;
        padding: 2px 4px;
        cursor: pointer;
        font-size: 12px;
        color: #64748b;
        border-radius: 3px;
      }

      .layer-item-btn:hover {
        background: #e2e8f0;
        color: #334155;
      }

      /* 热区配置抽屉 */
      .hotzone-drawer {
        position: absolute;
        top: 0;
        right: 0;
        bottom: 0;
        width: 320px;
        background: #ffffff;
        box-shadow: -4px 0 20px rgba(0, 0, 0, 0.1);
        display: flex;
        flex-direction: column;
        z-index: 200;
      }

      .hotzone-drawer-header {
        display: flex;
        align-items: center;
        justify-content: space-between;
        padding: 16px 20px;
        border-bottom: 1px solid #e2e8f0;
      }

      .hotzone-drawer-header h3 {
        margin: 0;
        font-size: 16px;
        font-weight: 600;
        color: #1e293b;
      }

      .hotzone-drawer-close {
        width: 28px;
        height: 28px;
        border: none;
        background: transparent;
        font-size: 20px;
        color: #64748b;
        cursor: pointer;
        border-radius: 4px;
        display: flex;
        align-items: center;
        justify-content: center;
      }

      .hotzone-drawer-close:hover {
        background: #f1f5f9;
        color: #1e293b;
      }

      .hotzone-drawer-body {
        flex: 1;
        padding: 20px;
        overflow-y: auto;
      }

      .hotzone-form-group {
        margin-bottom: 16px;
      }

      .hotzone-form-group label {
        display: block;
        font-size: 14px;
        font-weight: 500;
        color: #334155;
        margin-bottom: 6px;
      }

      .hotzone-form-group .required {
        color: #ef4444;
      }

      .hotzone-input,
      .hotzone-textarea {
        width: 100%;
        padding: 10px 12px;
        border: 1px solid #e2e8f0;
        border-radius: 6px;
        font-size: 14px;
        color: #1e293b;
        box-sizing: border-box;
        transition: border-color 0.2s, box-shadow 0.2s;
      }

      .hotzone-input:focus,
      .hotzone-textarea:focus {
        outline: none;
        border-color: ${this.config.themeColor || DEFAULT_THEME_COLOR};
        box-shadow: 0 0 0 3px ${this.config.themeColor || DEFAULT_THEME_COLOR}22;
      }

      .hotzone-textarea {
        min-height: 80px;
        resize: vertical;
      }

      .hotzone-drawer-footer {
        display: flex;
        gap: 12px;
        padding: 16px 20px;
        border-top: 1px solid #e2e8f0;
      }

      .hotzone-btn {
        flex: 1;
        padding: 10px 16px;
        border: none;
        border-radius: 6px;
        font-size: 14px;
        font-weight: 500;
        cursor: pointer;
        transition: background 0.2s;
      }

      .hotzone-btn-cancel {
        background: #f1f5f9;
        color: #475569;
      }

      .hotzone-btn-cancel:hover {
        background: #e2e8f0;
      }

      .hotzone-btn-save {
        background: ${this.config.themeColor || DEFAULT_THEME_COLOR};
        color: #ffffff;
      }

      .hotzone-btn-save:hover {
        filter: brightness(0.9);
      }

      /* ========== 形状库工具组 ========== */
      .shape-library-group .tool-dropdown {
        min-width: 280px;
        max-width: 320px;
        padding: 0;
      }

      .shape-library-header {
        padding: 10px 12px;
        font-size: 13px;
        font-weight: 600;
        color: #1e293b;
        background: #f8fafc;
        border-bottom: 1px solid #e2e8f0;
        display: flex;
        flex-direction: column;
        gap: 8px;
      }

      .shape-library-search {
        width: 100%;
        padding: 6px 10px;
        border: 1px solid #e2e8f0;
        border-radius: 6px;
        font-size: 12px;
        outline: none;
        transition: border-color 0.2s;
      }

      .shape-library-search:focus {
        border-color: var(--theme-color);
      }

      .shape-library-grid {
        display: grid;
        grid-template-columns: repeat(4, 1fr);
        gap: 6px;
        padding: 10px;
        min-height: 80px;
        max-height: 320px;
        overflow-y: auto;
        overflow-x: hidden;
      }

      .shape-library-grid:empty + .shape-library-empty {
        display: block;
      }

      .shape-library-empty {
        display: none;
        padding: 20px;
        text-align: center;
        color: #94a3b8;
        font-size: 12px;
      }

      .shape-library-no-results {
        grid-column: 1 / -1;
        padding: 20px;
        text-align: center;
        color: #94a3b8;
        font-size: 12px;
      }

      .shape-library-item {
        display: flex;
        flex-direction: column;
        align-items: center;
        justify-content: center;
        padding: 8px 4px;
        border: 2px solid transparent;
        border-radius: 8px;
        cursor: pointer;
        transition: all 0.15s;
        background: #f8fafc;
        aspect-ratio: 1;
      }

      .shape-library-item:hover {
        background: var(--theme-hover-bg);
        border-color: var(--theme-color);
      }

      .shape-library-item-preview {
        width: 32px;
        height: 32px;
        display: flex;
        align-items: center;
        justify-content: center;
      }

      .shape-library-item-preview svg {
        width: 100%;
        height: 100%;
      }

      .shape-library-item-name {
        font-size: 9px;
        color: #64748b;
        margin-top: 3px;
        text-align: center;
        overflow: hidden;
        text-overflow: ellipsis;
        white-space: nowrap;
        max-width: 100%;
      }

      /* ========== 移动端响应式布局 ========== */
      @media (max-width: 768px) {
        .editor-container {
          flex-direction: column;
        }

        .toolbar {
          width: 100%;
          height: auto;
          flex-direction: row;
          flex-wrap: wrap;
          border-right: none;
          border-bottom: 1px solid #e2e8f0;
          padding: 8px;
          gap: 4px;
          order: -1;
        }

        .tool-btn {
          width: 40px;
          height: 40px;
          min-width: 40px;
        }

        .divider {
          width: 1px;
          height: 24px;
          margin: 0 4px;
        }

        .property-panel {
          width: 100%;
          height: auto;
          max-height: 200px;
          border-left: none;
          border-top: 1px solid #e2e8f0;
          overflow-x: auto;
          flex-direction: row;
          flex-wrap: wrap;
        }

        .property-group {
          flex-direction: row;
          flex-wrap: wrap;
          padding: 8px;
        }

        .file-controls {
          flex-wrap: wrap;
        }

        .zoom-controls {
          flex-wrap: wrap;
        }

        .bottom-toolbar {
          flex-wrap: wrap;
          padding: 8px;
          gap: 8px;
        }
      }

      @media (max-width: 480px) {
        .toolbar {
          justify-content: center;
        }

        .tool-btn {
          width: 36px;
          height: 36px;
        }

        .property-panel {
          max-height: 150px;
        }

        .canvas-container {
          min-height: 300px;
        }
      }

      /* 触摸设备优化 */
      @media (pointer: coarse) {
        .tool-btn, .file-btn, .zoom-btn, .align-btn {
          min-width: 44px;
          min-height: 44px;
        }

        .tool-btn:active, .file-btn:active, .zoom-btn:active {
          transform: scale(0.95);
          background: ${hoverBgColor};
        }
      }
    `;
  }
}

// 注册 Web Component
if (typeof window !== 'undefined' && !customElements.get('canvas-drawing-editor')) {
  customElements.define('canvas-drawing-editor', CanvasDrawingEditor);
}
