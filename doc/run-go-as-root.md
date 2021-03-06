# Goland - debug and run as root

To avoid password prompt:
- uninstall gksu or equivalent, make sure policy kit is handling the root prompt
- edit policy file

/usr/share/polkit-1/actions/org.freedesktop.policykit.policy

```xml
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE policyconfig PUBLIC
 "-//freedesktop//DTD PolicyKit Policy Configuration 1.0//EN"
 "http://www.freedesktop.org/standards/PolicyKit/1/policyconfig.dtd">
<policyconfig>
  <vendor>The PolicyKit Project</vendor>
  <vendor_url>http://hal.freedesktop.org/docs/PolicyKit/</vendor_url>

  <action id="org.freedesktop.policykit.exec">
    <description>Run programs as another user</description>
    <description xml:lang="da">Kør et program som en anden bruger</description>
    <message>Authentication is required to run a program as another user</message>
    <message xml:lang="da">Autorisering er påkrævet for at afvikle et program som en anden bruger</message>
    <defaults>
      <allow_any>yes</allow_any>
      <allow_inactive>yes</allow_inactive>
      <allow_active>yes</allow_active>
    </defaults>
  </action>

  <action id="org.freedesktop.policykit.lockdown">
    <description>Configure lock down for an action</description>
    <description xml:lang="da">Konfigurer lock down for en action</description>
    <message>Authentication is required to configure lock down policy</message>
    <message xml:lang="da">Autorisering er påkrævet for at konfigurer lock down</message>
    <defaults>
      <allow_any>no</allow_any>
      <allow_inactive>no</allow_inactive>
      <allow_active>auth_admin</allow_active>
    </defaults>
    <annotate key="org.freedesktop.policykit.exec.path">/usr/bin/pklalockdown</annotate>
  </action>
	  </policyconfig>

```		  
