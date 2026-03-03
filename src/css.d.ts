// Ambient declaration for CSS module/side-effect imports (required by TypeScript 6)
declare module "*.css" {
  const content: Record<string, string>;
  export default content;
}
