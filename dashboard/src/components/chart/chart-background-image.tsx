import { produce } from "immer";
import { Dispatch, SetStateAction } from "react";
import { ChartValue, outerBaseUrl } from "./chart-type";

const PRESET_IMAGES: string[] = [];

interface ChartBackgroundImageProps {
  onChange: Dispatch<SetStateAction<ChartValue>>;
}

export default function ChartBackGroundImage({
  onChange,
}: ChartBackgroundImageProps) {
  return (
    <div className="grid grid-cols-3 gap-2 pt-2">
      {PRESET_IMAGES.map((image, index) => {
        return (
          <div key={index} className="relative cursor-pointer">
            <img
              src={outerBaseUrl + image}
              alt=""
              className="h-24 w-full rounded-lg object-cover"
              onClick={() => {
                onChange((prev) => {
                  return produce(prev, (draft) => {
                    draft.params.options.backgroundImage = image;
                    draft.params.options.backgroundType = "image";
                  });
                });
              }}
            />
          </div>
        );
      })}
    </div>
  );
}
