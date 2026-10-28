import Image from "next/image";
import { homeAssets } from "../../shared/header";

/** Supplied light and dark wordmarks, with their proportions preserved. */
export function BrandLogo({ priority = false }: { priority?: boolean }) {
  return <span className="wk-logo"><Image className="wk-logo-light" src={`${homeAssets}/brand/aethcraft-light.png`} alt="AethCraft" width={1032} height={250} priority={priority} /><Image className="wk-logo-dark" src={`${homeAssets}/brand/aethcraft-dark.png`} alt="" aria-hidden="true" width={1032} height={250} priority={priority} /></span>;
}
