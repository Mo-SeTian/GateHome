import unittest
from unittest.mock import patch

import wireguard_host as host


class WireGuardTest(unittest.TestCase):
    config = dict(endpoint="vpn.example.test", lan="192.168.2.0/24", tunnel="10.77.0.0/24",
                  port=51820, snat=True, admin_port=16666)

    def test_independent_tunnel_and_home_routes(self):
        with patch.object(host, "networks", return_value=["192.168.2.0/24"]), patch.object(host, "command", return_value="[]"):
            self.assertEqual(host.settings(self.config, None, []), self.config)
            with self.assertRaises(host.Invalid):
                host.settings(dict(self.config, endpoint="fe80::1%eth0"), None, [])
            for tunnel in ("192.168.2.0/24", "0.0.0.0/0", "10.77.0.0/31"):
                with self.assertRaises(host.Invalid):
                    host.settings(dict(self.config, tunnel=tunnel), None, [])
            with self.assertRaises(host.Invalid):
                host.settings(dict(self.config, lan="192.168.3.0/24"), self.config, [object()])

    def test_restrict_device_access_and_keep_identity(self):
        original = dict(name="phone", address="10.77.0.2", id="peer")
        body = dict(name="phone", mode="host", target="192.168.2.60", port=7777)
        peer, key = host.device(body, original, self.config, [original])
        self.assertIsNone(key)
        self.assertEqual(peer["address"], original["address"])
        self.assertEqual(host.allowed(peer, self.config), "192.168.2.60/32")
        rules = host.firewall(self.config, [peer])
        self.assertIn("ip saddr 10.77.0.2 ip daddr 192.168.2.60/32 tcp dport 7777 accept", rules)
        self.assertIn("fib daddr type local tcp dport 16666 drop", rules)
        self.assertNotIn("ip daddr 192.168.2.0/24 accept", rules)
        self.assertNotIn("masquerade", host.firewall(dict(self.config, snat=False), [peer]))
        with self.assertRaises(host.Invalid):
            host.device(dict(body, target="192.168.3.3"), original, self.config, [original])

    def test_failed_change_restores_previous_policy(self):
        old = {"settings": self.config, "devices": [{"id": "test-device"}]}
        with patch.object(host, "state", old), patch.object(host, "apply", side_effect=[host.Invalid("error"), None]) as apply, patch.object(host, "save") as save:
            with self.assertRaises(host.Invalid):
                host.mutate("remove", {"id":"test-device"})
            save.assert_not_called()
            self.assertEqual(apply.call_args_list[-1].args[0], old)
            self.assertEqual(host.state["devices"], [{"id":"test-device"}])
