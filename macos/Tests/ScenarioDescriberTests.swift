import XCTest

final class ScenarioDescriberTests: XCTestCase {
    func testPartialBindingDoesNotDescribeTheWholeRule() {
        var profile = Profile()
        profile.lines = [
            Line(id: "direct", name: "直连", type: "direct"),
            Line(id: "proxy", name: "Proxy", type: "socks5"),
        ]
        profile.ruleSets = [RuleSet(id: "sites", name: "Sites", type: "domain")]
        var scenario = Scenario(
            id: "selected", name: "Selected",
            bindings: [RuleBinding(ruleSetID: "sites", lineID: "proxy", conditionIDs: ["one"])],
            defaultLineID: "direct"
        )
        XCTAssertEqual(
            ScenarioDescriber.summary(for: scenario, in: profile, isChinese: false),
            "Sites (partial match) via Proxy, everything else direct"
        )
        XCTAssertEqual(
            ScenarioDescriber.summary(for: scenario, in: profile, isChinese: true),
            "Sites（部分匹配）走Proxy，其余直连"
        )
        scenario.bindings[0].conditionIDs = []
        XCTAssertEqual(
            ScenarioDescriber.summary(for: scenario, in: profile, isChinese: false),
            "Sites via Proxy, everything else direct"
        )
    }
}
