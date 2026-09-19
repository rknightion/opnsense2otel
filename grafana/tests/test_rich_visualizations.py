import sys
import unittest
from pathlib import Path


GRAFANA_DIR = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(GRAFANA_DIR))

from builder import Builder, loki_grp, loki_sel, sel  # noqa: E402
from tabs import flow  # noqa: E402


def panel(builder, name):
    return builder.elements[name]["spec"]


def query_model(spec, index=0):
    return spec["data"]["spec"]["queries"][index]["spec"]["query"]


class RichVisualizationBuilderTest(unittest.TestCase):
    def setUp(self):
        self.b = Builder()
        self.prom = f'sum {"by (opnsense_instance, protocol)"} ({sel("opnsense_flow_bytes_total")})'
        self.log = (
            f'topk {loki_grp()} (20, sum {loki_grp("src_scope", "dst_scope")} '
            f'(count_over_time({loki_sel("opnsense_source=\"netflow\"")}[$__range])))'
        )

    def assert_panel(self, name, group, size, datasource, instant):
        spec = panel(self.b, name)
        self.assertEqual(spec["vizConfig"]["group"], group)
        self.assertEqual(self.b.size[name], size)
        query = query_model(spec)
        self.assertEqual(query["group"], datasource)
        model = query["spec"]
        self.assertEqual(model["instant"], instant)
        self.assertEqual(model["range"], not instant)
        identity = "opnsense_instance" if datasource == "prometheus" else "service_instance_id"
        self.assertIn(identity, model["expr"])

    def test_builtin_rich_visualizations_encode_query_and_layout_contracts(self):
        names = {
            "bar": self.b.barchart("Traffic by protocol", [(self.prom, "{{protocol}}")]),
            "heatmap": self.b.heatmap("Flow size distribution", self.prom),
            "histogram": self.b.histogram("Current flow sizes", [(self.prom, "{{protocol}}")]),
            "geomap": self.b.geomap(
                "Destinations", self.prom, location_field="country", value_field="Value"
            ),
            "xy": self.b.xychart("Traffic relationship", [(self.prom, "{{protocol}}")]),
            "node": self.b.nodegraph("Traffic topology", self.log, datasource="loki"),
        }

        self.assert_panel(names["bar"], "barchart", (12, 8), "prometheus", True)
        self.assert_panel(names["heatmap"], "heatmap", (12, 8), "prometheus", False)
        self.assertEqual(query_model(panel(self.b, names["heatmap"]))["spec"]["format"],
                         "heatmap")
        self.assert_panel(names["histogram"], "histogram", (12, 8), "prometheus", False)
        self.assert_panel(names["geomap"], "geomap", (12, 10), "prometheus", True)
        self.assert_panel(names["xy"], "xychart", (12, 8), "prometheus", False)
        self.assert_panel(names["node"], "nodeGraph", (24, 10), "loki", True)
        layer = panel(self.b, names["geomap"])["vizConfig"]["spec"]["options"]["layers"][0]
        self.assertEqual(layer["location"]["mode"], "lookup")
        self.assertEqual(layer["location"]["lookup"], "country")

    def test_catalogue_panels_use_exact_plugin_ids_and_keep_transformations(self):
        transforms = [{"kind": "Transformation", "group": "organize", "spec": {"options": {}}}]
        names = {
            "sankey": self.b.sankey(
                "Traffic paths", self.log, datasource="loki", transformations=transforms
            ),
            "treemap": self.b.treemap("Applications", self.log, datasource="loki"),
            "polystat": self.b.polystat("Collector health", [(self.prom, "{{protocol}}")]),
        }

        self.assert_panel(names["sankey"], "netsage-sankey-panel", (24, 10), "loki", True)
        self.assert_panel(names["treemap"], "marcusolsson-treemap-panel", (12, 10), "loki", True)
        self.assert_panel(names["polystat"], "grafana-polystat-panel", (12, 8), "prometheus", True)
        self.assertEqual(panel(self.b, names["sankey"])["data"]["spec"]["transformations"], transforms)


class FlowRichVisualizationTest(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.b = Builder()
        flow.build(cls.b)
        cls.panels = {
            value["spec"]["title"]: value["spec"]
            for value in cls.b.elements.values()
            if value["kind"] == "Panel"
        }

    def test_flow_volume_has_relationship_categorical_spatial_and_hierarchical_views(self):
        expected = {
            "Traffic Paths by Scope": "netsage-sankey-panel",
            "Application Categories by Throughput": "barchart",
            "Remote Traffic Geography": "geomap",
            "Applications by Bytes": "marcusolsson-treemap-panel",
        }
        for title, group in expected.items():
            with self.subTest(title=title):
                self.assertIn(title, self.panels)
                self.assertEqual(self.panels[title]["vizConfig"]["group"], group)

    def test_flow_rich_queries_remain_bounded_and_instance_aware(self):
        for title in ("Traffic Paths by Scope", "Applications by Bytes"):
            model = query_model(self.panels[title])
            self.assertTrue(model["spec"]["instant"])
            self.assertIn("topk by (service_instance_id)", model["spec"]["expr"])
            self.assertIn("service_instance_id", model["spec"]["expr"])

        for title in ("Application Categories by Throughput", "Remote Traffic Geography"):
            expr = query_model(self.panels[title])["spec"]["expr"]
            self.assertIn("opnsense_instance", expr)

    def test_plugin_panels_have_builtin_siblings_in_the_same_conditional_rows(self):
        rows = self.b.tabs[-1]["spec"]["layout"]["spec"]["rows"]
        groups = []
        for row in rows:
            names = [item["spec"]["element"]["name"]
                     for item in row["spec"]["layout"]["spec"]["items"]]
            titles = {self.b.elements[name]["spec"]["title"] for name in names}
            groups.append(titles)
        self.assertTrue(any({"Traffic Paths by Scope", "Raw Flow Records"} <= titles
                            for titles in groups))
        self.assertTrue(any({"Applications by Bytes", "Top Flow Applications by Bytes"} <= titles
                            for titles in groups))


if __name__ == "__main__":
    unittest.main()
