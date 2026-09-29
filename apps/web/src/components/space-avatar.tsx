import { useRef } from "react";
import { useLingui } from "@lingui/react/macro";
import { Camera, X } from "lucide-react";
import { toast } from "sonner";
import { Avatar, AvatarFallback, AvatarImage } from "@workspace/ui/components/avatar";
import { Button } from "@workspace/ui/components/button";
import { useClearSpaceAvatar, useUploadSpaceAvatar } from "@/lib/queries";

interface SpaceAvatarProps {
  /** Required only when `editable`: the mutations address the space. */
  spaceId?: string;
  src?: string;
  name?: string;
  size?: "sm" | "md" | "lg";
  /** Shows the camera control, and a remove control while a picture is set. */
  editable?: boolean;
}

const sizeClasses = {
  // eslint-disable-next-line lingui/no-unlocalized-strings -- Tailwind class names
  sm: "h-8 w-8",
  // eslint-disable-next-line lingui/no-unlocalized-strings -- Tailwind class names
  md: "h-16 w-16",
  // eslint-disable-next-line lingui/no-unlocalized-strings -- Tailwind class names
  lg: "h-24 w-24",
};

/** A space's face: its picture, or its initials when it has none. */
export function SpaceAvatar({
  spaceId,
  src,
  name,
  size = "md",
  editable = false,
}: SpaceAvatarProps) {
  const { t } = useLingui();
  const inputRef = useRef<HTMLInputElement>(null);
  const upload = useUploadSpaceAvatar(spaceId ?? "");
  const clear = useClearSpaceAvatar(spaceId ?? "");

  const initials = (name || "?").slice(0, 2).toUpperCase();

  const handleFileChange = (e: React.ChangeEvent<HTMLInputElement>) => {
    const file = e.target.files?.[0];
    if (!file || !spaceId) return;
    upload.mutate(file, {
      onSuccess: () => toast.success(t`Space avatar updated`),
      onError: (err) =>
        toast.error(t`Could not update the space avatar`, { description: err.message }),
    });
    // Allow picking the same file again after a failed upload.
    e.target.value = "";
  };

  return (
    <div className="relative">
      <Avatar className={sizeClasses[size]}>
        <AvatarImage src={src} />
        <AvatarFallback className={size === "lg" ? "text-2xl" : "text-sm"}>
          {initials}
        </AvatarFallback>
      </Avatar>
      {editable && (
        <>
          <input
            ref={inputRef}
            type="file"
            accept="image/*"
            className="hidden"
            onChange={handleFileChange}
          />
          <Button
            size="icon"
            variant="secondary"
            className="absolute -bottom-1 -right-1 h-6 w-6 rounded-full"
            aria-label={t`Change space avatar`}
            onClick={() => inputRef.current?.click()}
            disabled={upload.isPending}
          >
            <Camera className="h-3 w-3" />
          </Button>
          {src && (
            <Button
              size="icon"
              variant="secondary"
              className="absolute -top-1 -right-1 h-6 w-6 rounded-full"
              aria-label={t`Remove space avatar`}
              onClick={() =>
                clear.mutate(undefined, {
                  onSuccess: () => toast.success(t`Space avatar removed`),
                  onError: (err) =>
                    toast.error(t`Could not remove the space avatar`, { description: err.message }),
                })
              }
              disabled={clear.isPending}
            >
              <X className="h-3 w-3" />
            </Button>
          )}
        </>
      )}
    </div>
  );
}
