import { useRef } from "react";
import { useLingui } from "@lingui/react/macro";
import { Camera } from "lucide-react";
import { toast } from "sonner";
import { Avatar, AvatarFallback, AvatarImage } from "@workspace/ui/components/avatar";
import { Button } from "@workspace/ui/components/button";
import { useUploadAvatar } from "@/lib/queries";

interface UserAvatarProps {
  src?: string;
  name?: string;
  email?: string;
  size?: "sm" | "md" | "lg";
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

export function UserAvatar({ src, name, email, size = "md", editable = false }: UserAvatarProps) {
  const { t } = useLingui();
  const inputRef = useRef<HTMLInputElement>(null);
  const upload = useUploadAvatar();

  const initials = (name || email || "?").slice(0, 2).toUpperCase();

  const handleFileChange = (e: React.ChangeEvent<HTMLInputElement>) => {
    const file = e.target.files?.[0];
    if (!file) return;
    upload.mutate(file, {
      onSuccess: () => toast.success(t`Avatar updated`),
      onError: (err) => toast.error(t`Could not update your avatar`, { description: err.message }),
    });
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
            onClick={() => inputRef.current?.click()}
            disabled={upload.isPending}
          >
            <Camera className="h-3 w-3" />
          </Button>
        </>
      )}
    </div>
  );
}
