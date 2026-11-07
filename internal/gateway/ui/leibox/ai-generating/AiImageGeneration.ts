import { mountAiImageGeneration, type AiImageGenerationOptions } from "./AiImageGenerationEngine.js";
import type { MotionController } from "./motion-runtime";

/** Mount into any DOM node. Call update() for options and destroy() on removal. */
export function mount(target: HTMLElement, options: AiImageGenerationOptions = {}): MotionController {
  return mountAiImageGeneration(target, options);
}
export type { AiImageGenerationOptions };
