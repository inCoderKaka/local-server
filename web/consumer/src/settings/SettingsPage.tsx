import { MySettings } from './MySettings';
import { PasskeyForm } from './PasskeyForm';
import { PasswordForm } from './PasswordForm';

/** Settings: the consumer-owned settings, the password change and adding a passkey. */
export default function SettingsPage() {
  return (
    <>
      <h1 className="text-xl font-semibold">Settings</h1>
      <PasswordForm />
      <PasskeyForm />
      <MySettings />
    </>
  );
}
